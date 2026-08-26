// Command mps runs the Mario Party Superstars online servers (auth + secure) on the
// Nextendo NEX stack — a from-scratch NEX implementation with no third-party dependencies.
// It runs the auth and secure servers in one process.
//
// Two NEX servers run in one process:
//   - auth   (:443)     TicketGranting — LoginEx issues the Kerberos ticket.
//   - secure (:60011)   SecureConnection + matchmaking + NAT traversal + Utility + Ranking.
//
// STATUS: scaffolded from the proven `arms` template, not yet tested against a real
// client at all. Unlike every other title in this org, NOTHING here has been confirmed on
// the wire yet — there is no public Game Server List entry for this title (checked
// kinnay/NintendoClients' wiki, 2026-08-18), so every value below is either a placeholder
// or a same-era guess. See README.md for what's needed before this can work at all.
//
// Every value that could not be confirmed is behind an env var (MPS_*) so it can be
// flipped without recompiling. See example.env and README.md.
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"os"
	"strconv"
	"strings"
	"time"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

const (
	// defaultAccessKey: CONFIRMED (2026-08-18) via brute-force against a real PRUDP
	// CONNECT signature captured from a live citron connection attempt -- mathematically
	// certain, not inferred. "28abaa00" (captured from the DNS-resolve hostname
	// "g28abaa00-lp1.s.n.srv.nintendo.net") turned out to be the Game Server ID, NOT the
	// access key -- these are two separate values per kinnay's NintendoClients wiki
	// convention (confirmed by checking this org's own deployed ARMS server: its Game
	// Server ID is 25c08801 but its real ACCESS_KEY is b6b34c51, a totally different
	// value). Since access keys are always exactly 8 hex digits (a 2^32 space), and the
	// PRUDP-Lite signature is a pure function of (accessKey, connectionSig) --
	// HMAC-MD5(MD5(accessKey), MD5(accessKey)+connectionSig) -- capturing one real
	// (connectionSig, client signature) pair from a live connection attempt let a
	// brute-force search over all 2^32 candidates find the exact key directly, with zero
	// ambiguity. See README.md for the full trail.
	defaultAccessKey = "e915510f"

	// defaultNexVersion: CONFIRMED via direct binary inspection (2026-08-18) -- main's
	// .rodata carries "SDK MW+Nintendo+NEX-4_6_5-" in the same SDK-build-version string
	// table as the rest of the NintendoWare/NEX/Pia component list (also confirms
	// Pia 5.33.0 -- newer than every other title in this org, none of which go past
	// Pia 5.19). This is real data, not a guess, unlike the access key below.
	defaultNexVersion = 40605

	securePID     = 2
	sessionKeyLen = 32

	// mpsTitleID -- confirmed via multiple independent NSP/XCI filename sources
	// (2026-08-18): 01006FE013472000. Unlike the access key, this one is solid --
	// title IDs are trivially public and consistently reported everywhere.
	mpsTitleID = "01006fe013472000"
)

var (
	// accessKey / nexVersion — see the const block above for provenance (or lack of it).
	accessKey  = envOr("MPS_ACCESS_KEY", defaultAccessKey)
	nexVersion = envOrInt("MPS_NEX_VERSION", defaultNexVersion)

	// nextendoHost is the BARE IP the console will dial — NOT host:port. It is used only as
	// the "address" param of the secure station URL; the port comes from SECURE_PORT.
	nextendoHost = envOr("NEXTENDO_HOST", "127.0.0.1")
	authPort     = envOrInt("AUTH_PORT", 443)
	securePort   = envOrInt("SECURE_PORT", 60011)

	securePassword = envOr("NEXTENDO_SECURE_PASSWORD", "securepasswordplz1")
	certFile       = envOr("CERT_FILE", "cert.pem")
	keyFile        = envOr("KEY_FILE", "key.pem")

	// nextendoSecret signs "nx2." NEX login tokens issued by the account service. It MUST
	// be byte-identical to nextendo-account's secret or token validation fails.
	nextendoSecret = loadNextendoSecret()
	// requireAccount, when "1", rejects any login without a valid Nextendo token.
	requireAccount = os.Getenv("NEXTENDO_REQUIRE_ACCOUNT") == "1"
)

// --- Unverified wire-shape knobs. Defaults = the MODERN (Switch Pia 5.19) profile, the
// --- shape SSBU/MK8 use, since Mario Party Superstars (2021) is later than every title in
// --- this org that needs the LEGACY (pre-Pia-5.19) shape (ARMS, Splatoon 2). This is a
// --- guess, not a measurement -- nothing about MPS's actual Pia version has been checked.
// --- Each is an env var so a wrong guess costs a restart, not a recompile.

// stationScheme: "prudps" (PRUDP *Secure*) makes the console treat the secure server as
// authenticated and hand over its Kerberos ticket in CONNECT. With "prudp" the handshake
// completes but CONNECT carries an EMPTY payload — no ticket, no session key — and the
// title dies the moment it needs the session. MK8 is the one outlier in this org that
// still uses "prudp"; everything else (S2, SSBU, ARMS) uses "prudps". Defaulting to
// "prudps" as the more common case.
func stationScheme() string { return envOr("MPS_STATION_SCHEME", "prudps") }

// secureMinor: the PRUDP minor version the SECURE endpoint answers SYN with. The retail
// secure server answered 0 where nextendo-nex defaults to 5; with 5 the console sends
// CONNECT with plen=0 and never hands over the ticket. NEVER force this on the auth
// endpoint.
func secureMinor() int { return envOrInt("MPS_SECURE_MINOR", 0) }

// legacyPia: true = legacy type=0x03 / no-Pa public station shape (ARMS, Splatoon 2,
// pre-Pia-5.19). false = the Switch Pia 5.19 shape (type=0x0B + Pa) SSBU/MK8 use. MPS is
// CONFIRMED on Pia 5.33.0 (see defaultNexVersion's comment) -- well past 5.19, so false
// (modern) is not just a guess here, unlike everything else in this block. Still kept
// overridable in case Pia 5.33 changed shape again since 5.19; nothing in this org's NEX
// stack has been tested against anything that new yet.
func legacyPia() bool { return envOr("MPS_LEGACY_PIA", "0") == "1" }

func main() {
	settings := nex.NewSwitchSettings(accessKey, nexVersion)

	// --- Auth server (:443) ---
	secureURL := nex.NewStationURL(stationScheme())
	secureURL.Set("address", nextendoHost)
	secureURL.SetInt("port", securePort)
	secureURL.SetInt("CID", 1)
	secureURL.SetInt("PID", securePID)
	secureURL.SetInt("sid", 1)
	secureURL.SetInt("stream", 10)
	secureURL.SetInt("type", 2) // public

	authEndpoint := nex.NewEndpoint(settings)
	authCfg := &nex.AuthConfig{
		Settings:         settings,
		SecurePID:        securePID,
		SecurePassword:   securePassword,
		SecureStationURL: secureURL,
		ServerName:       "Nextendo",
		SessionKeyLength: sessionKeyLen,
		ResolveUser:      resolveUser,
	}
	authEndpoint.Register(nex.ProtocolTicketGranting, authCfg.Handler())
	authEndpoint.OnRMC = logRMC("Auth")
	authServer := nex.NewServer(authEndpoint)

	// --- Secure server (:60011) ---
	secureSettings := nex.NewSwitchSettings(accessKey, nexVersion)
	secureSettings.PrudpMinorVersion = secureMinor()
	secureEndpoint := nex.NewEndpoint(secureSettings)
	secureEndpoint.SetSecureAccount(securePassword, securePID)

	mm := nex.NewMatchmaking()
	// FindMatchmakeSessionByParticipant (0x6D method 51) defaults to an empty answer -- ACNH
	// is the only title that opts in today. MPS's "friend could not see my lobby" bug is this:
	// the joiner's client asks the server "what gathering is PID <host> currently in?" via this
	// method to join the HOST's existing session; answered empty, it falls back to public
	// autoMatchmake and creates its own brand-new session instead. MPS needs a real answer here
	// for friend-invite/join-lobby to work at all.
	mm.FindByParticipantEnabled = true
	// PublicStationFirst / JoinRespExistingCount: previously forced on here by analogy to
	// ACNH ("MPS uses the same SwitchPia519Config as ACNH, so it needs ACNH's fix"). That
	// analogy doesn't hold -- SSBU uses this exact same Pia shape and is proven to need
	// BOTH flags at their library default (false): SSBU keeps the after-add participant
	// count and the [lan, public] station order, same as MK8/S2. A live autoMatchmake test
	// on 2026-08-19 (real 2-client session, gid formed, one side's hole-punch completed,
	// the other never reported a result -- the exact "one or more consoles are not
	// responding" shape) traced back to this: MPS was on ACNH's branch of this fork instead
	// of MK8/SSBU's. Reverting to the proven MK8/SSBU default for both.
	mm.PublicStationFirst = false
	mm.JoinRespExistingCount = false
	// FriendPIDs/FriendName/OnFriendSessionCreated: the "Join Room" screen polls
	// FindMatchmakeSessionByGatheringIdDetail (method 41) against a gid it gets from friend
	// presence data, NOT from browse/autoMatchmake -- confirmed live tonight: the joiner sat on
	// that screen polling method 41 once a second for 3 minutes and got RendezVousSessionVoid
	// every single time, because nobody had ever told it which gid the host was on. See
	// friends.go (ported from luigis-mansion-3, the only other title that already wires this).
	mm.FriendPIDs = accountFriendPIDs
	mm.FriendName = dispName
	mm.OnFriendSessionCreated = func(pid uint64, gid uint32) {
		publishFriendSession(mm, pid, gid)
	}

	scCfg := nex.LegacyPiaConfig()
	if !legacyPia() {
		scCfg = nex.SwitchPia519Config()
	}
	secureEndpoint.Register(nex.ProtocolSecureConnection, nex.SecureConnectionHandlerWithConfig(scCfg))
	secureEndpoint.Register(nex.ProtocolMatchmakeExtension, mm.ExtensionHandler())
	secureEndpoint.Register(nex.ProtocolMatchMaking, mm.MatchMakingHandler())
	secureEndpoint.Register(nex.ProtocolMatchMakingExt, mm.MatchMakingExtHandler())
	secureEndpoint.Register(nex.ProtocolNATTraversal, nex.NATTraversalHandler())
	// Utility (0x6E): NOT the generic nex.UtilityHandler() -- confirmed via kinnay's
	// NintendoClients wiki (Utility-Protocol page) that GetIntegerSettings takes a single
	// index parameter and must return a REAL populated Map<u16,s32>, not an empty list. The
	// generic handler misparsed the request entirely and always answered empty, which is
	// consistent with MPS's secure connection dying ~1s after Register+GetIntegerSettings
	// every single time tonight. See utility_stub.go.
	secureEndpoint.Register(nex.ProtocolUtility, mpsUtilityHandler())
	// DataStore (0x73): no handler at all was registered here before, unlike every other
	// protocol above. SSBU (also NEX 4.6+) hit this exact gap and soft-locked -- reconnecting
	// from scratch instead of proceeding -- until a stub was registered. See datastore_stub.go.
	secureEndpoint.Register(protocolDataStore, dataStoreStubHandler())
	// Pia type-8 keepalive (~3s period) -- unACKed, the console treats the connection as dead.
	// See pia_keepalive.go.
	setupMPSType8Keepalive(secureEndpoint)
	// [Nextendo] Diagnostic only, 2026-08-19: the real client links two protocols we register
	// NO handler for at all -- Subscriber, and (per kinnay's NEX-Internal-Protocols doc)
	// NATTraversalReportInternal, which is where ReportNATProperties/ReportNATTraversalResult
	// may actually live instead of on the main NATTraversal protocol (0x03) where we currently
	// answer them. A join consistently fails almost instantly on exactly one side (host vs
	// joiner, not device-specific) with no NAT result ever reported for that side -- consistent
	// with the client calling a protocol id we silently NotImplemented. This just logs what
	// protocol/method actually gets called with no handler, still answering NotImplemented same
	// as before -- purely observational, changes no existing behavior.
	secureEndpoint.RegisterFallback(func(conn *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		fmt.Printf("[MPS Unhandled] pid=%d proto=%#x method=%d call=%d bodyLen=%d -- NO HANDLER REGISTERED\n",
			conn.PID, req.Protocol, req.Method, req.CallID, len(req.Body))
		return nex.NewRMCError(conn.Settings, req.Protocol, req.CallID, nex.ResultCoreNotImplemented)
	})
	// Ranking (0x70): party games commonly track minigame/board-game high scores through
	// this protocol. Registered with the generic handler, same as ARMS -- if MPS actually
	// calls it for real leaderboard data, this will need a title-specific implementation,
	// not just the generic ack.
	secureEndpoint.Register(nex.ProtocolRanking, nex.RankingHandler())

	logSecure := logRMC("Secure")
	secureEndpoint.OnRMC = func(c *nex.Connection, req *nex.RMCMessage) {
		logSecure(c, req)
		noteRMC(c, req)         // feed the monitoring dashboard
		notePresenceSeen(c.PID) // any packet from a PID = that account is playing MPS now
	}
	secureEndpoint.OnConnect = func(c *nex.Connection) {
		fmt.Printf("[MPS Secure] connected pid=%d id=%d addr=%s\n", c.PID, c.ID, c.RemoteAddr)
	}
	// Drop the player's lobbies when the connection dies. A gathering is otherwise only
	// removed when the client politely calls UnregisterGathering / EndParticipation, so a
	// client that crashes or errors out leaks its lobby forever.
	secureEndpoint.OnDisconnect = func(c *nex.Connection) {
		mm.RemovePlayer(c.PID)
	}
	secureServer := nex.NewServer(secureEndpoint)

	// Éviction automatique des connexions mortes + monitoring /api/stats.
	secureEndpoint.StartReaper()
	go startDashboard(secureEndpoint, mm)
	startPresenceReporter()

	// When the auth is fronted by a TLS-passthrough proxy (Traefik on the shared :443),
	// enable PROXY protocol so the auth sees the console's REAL IP. Not used locally.
	proxyProto := os.Getenv("NEXTENDO_PROXY_PROTOCOL") == "1"
	go func() {
		fmt.Printf("[MPS Auth] listening WSS :%d (proxyProto=%v, secure URL -> %s)\n", authPort, proxyProto, secureURL.String())
		var err error
		if proxyProto {
			err = authServer.ListenSecureProxy(authPort, certFile, keyFile)
		} else {
			err = authServer.ListenSecure(authPort, certFile, keyFile)
		}
		if err != nil {
			fmt.Printf("[MPS Auth] stopped: %v\n", err)
		}
	}()

	fmt.Printf("[MPS Secure] listening WSS :%d (accessKey=%s nexVersion=%d scheme=%s minor=%d legacyPia=%v title=%s)\n",
		securePort, accessKey, nexVersion, stationScheme(), secureMinor(), legacyPia(), mpsTitleID)
	if err := secureServer.ListenSecure(securePort, certFile, keyFile); err != nil {
		fmt.Printf("[MPS Secure] stopped: %v\n", err)
	}
}

// resolveUser maps a LoginEx username to an account. A valid "nx2." Nextendo token
// resolves to its persistent PID; anything else gets a stable anonymous PID derived from
// the username (so the same console keeps the same identity).
func resolveUser(username string, _ []byte) (uint64, []byte, bool) {
	// The source key encrypts the client ticket and is handed back as pSourceKey, so the
	// console decrypts it. It MUST be 32 bytes (the Switch kerberos key size).
	sk := sha256.Sum256([]byte("nextendo-src:" + username))
	sourceKey := sk[:]

	// 1. Signed nx2 token → the account's PERSISTENT PID (+ online gates).
	if pid, ok := nextendoPIDFromToken(username); ok {
		if allow, reason := nextendoOnlineCheck(pid, "ryujinx"); !allow {
			fmt.Printf("[Auth] pid=%d online REFUSÉ (%s)\n", pid, reason)
			return 0, nil, false
		}
		return pid, sourceKey, true
	}

	// 2. Numeric username. The emulator's "Connexion Nextendo" button sends the account's
	// OWN PID; a real CFW Switch sends its console baasUserID (a large NSA id) instead,
	// which we resolve to the account PID.
	if n, err := strconv.ParseUint(username, 10, 64); err == nil && n >= 1800000000 {
		if requireSignedToken() {
			fmt.Printf("[Auth] pid=%d REFUSE : identite par PID nu desactivee (jeton nx2 signe requis)\n", n)
			return 0, nil, false
		}
		fmt.Printf("[Auth] pid=%d identite par PID NU (non authentifiee — cf. NEXTENDO_REQUIRE_SIGNED_TOKEN)\n", n)
		pid, kind := n, "ryujinx"
		if n >= 1810000000 { // vraie Switch : NSA id -> PID de compte (online = comptes Nextendo UNIQUEMENT)
			kind = "switch"
			rp, st := resolveNSAtoPID(n)
			switch st {
			case nsaOK:
				pid = rp
				fmt.Printf("[Auth] NSA %d -> account pid=%d\n", n, pid)
			case nsaUnknown:
				fmt.Printf("[Auth] NSA %d REFUSÉ (aucun compte Nextendo)\n", n)
				return 0, nil, false
			case nsaUnreachable:
				fmt.Printf("[Auth] NSA %d REFUSÉ (serveur compte injoignable)\n", n)
				return 0, nil, false
			}
		}
		if allow, reason := nextendoOnlineCheck(pid, kind); !allow {
			fmt.Printf("[Auth] pid=%d online REFUSÉ (%s)\n", pid, reason)
			return 0, nil, false
		}
		return pid, sourceKey, true
	}

	// 3. Anonymous / no Nextendo identity.
	if requireAccount {
		fmt.Printf("[Auth] login anonyme REFUSÉ (compte Nextendo requis): %q\n", username)
		return 0, nil, false
	}
	return anonymousPID(username), sourceKey, true
}

// nextendoPIDFromToken validates a "nx2.<b64(pid.username.expiry)>.<b64(hmac)>" token
// signed by the account service (HMAC-SHA256, "nex:" prefix).
func nextendoPIDFromToken(s string) (uint64, bool) {
	if len(nextendoSecret) == 0 || !strings.HasPrefix(s, "nx2.") {
		return 0, false
	}
	parts := strings.Split(s[len("nx2."):], ".")
	if len(parts) != 2 {
		return 0, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, false
	}
	mac := hmac.New(sha256.New, nextendoSecret)
	mac.Write([]byte("nex:" + string(raw)))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(parts[1])) {
		return 0, false
	}
	f := strings.SplitN(string(raw), ".", 3) // pid.username.expiry
	if len(f) != 3 {
		return 0, false
	}
	pid, err := strconv.ParseUint(f[0], 10, 64)
	if err != nil {
		return 0, false
	}
	if exp, err := strconv.ParseInt(f[2], 10, 64); err != nil || time.Now().Unix() > exp {
		return 0, false
	}
	return pid, true
}

// loadNextendoSecret loads the shared NEX-token signing secret the SAME way
// nextendo-account does (its loadSecret): env NEXTENDO_SECRET as raw bytes if set,
// otherwise hex-decode the shared key file.
func loadNextendoSecret() []byte {
	if v := os.Getenv("NEXTENDO_SECRET"); v != "" {
		return []byte(v)
	}
	path := envOr("NEXTENDO_SECRET_FILE", "nextendo_secret.key")
	if b, err := os.ReadFile(path); err == nil {
		if dec, derr := hex.DecodeString(strings.TrimSpace(string(b))); derr == nil && len(dec) >= 16 {
			return dec
		}
	}
	return nil
}

// anonymousPID derives a stable PID in the NEX user range from a username.
func anonymousPID(username string) uint64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(username))
	return 1800000000 + uint64(h.Sum32()%100000000)
}

func logRMC(tag string) func(*nex.Connection, *nex.RMCMessage) {
	return func(c *nex.Connection, req *nex.RMCMessage) {
		fmt.Printf("[MPS %s] pid=%d proto=%#x method=%d call=%d\n", tag, c.PID, req.Protocol, req.Method, req.CallID)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// requireSignedToken : quand true, seule une identite prouvee par un jeton nx2 SIGNE est
// acceptee au LoginEx ; un PID nu est refuse. Desactive par defaut car l emulateur
// actuellement distribue envoie encore le PID nu.
func requireSignedToken() bool {
	v := os.Getenv("NEXTENDO_REQUIRE_SIGNED_TOKEN")
	return v == "1" || v == "true"
}
