<h1 align="center">mario-party-superstars</h1>

<p align="center">
  <b>Nextendo Network game server for Mario Party Superstars.</b>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/license-PolyForm%20Shield%201.0.0-orange" alt="License: PolyForm Shield 1.0.0">
  <img src="https://img.shields.io/badge/go-1.23%2B-00ADD8" alt="Go 1.23+">
</p>

---

## What is this?

The NEX game server for **Mario Party Superstars** (title ID `01006FE013472000`) on
[Nextendo Network](https://nextendo.network). It handles authentication and matchmaking,
speaking the same NEX protocol the retail servers did.

It is built on the [**nextendo-nex**](https://github.com/NextendoNetwork/nextendo-nex) core
(PRUDP transport, RMC layer, common service protocols) and follows the same shape as
[`arms`](https://github.com/NextendoNetwork/arms): auth (`:443`) + secure NEX endpoints in one
process, P2P gameplay once matched.

**Status: all identity/wire-shape values confirmed, deployed and running.** NEX version, Pia
version, and the access key are all real, confirmed values (see below).

## Identity / wire-shape values (all confirmed, not guesses)

### 1. The NEX access key — CONFIRMED `e915510f`

`MPS_ACCESS_KEY=e915510f` in [`example.env`](example.env), confirmed mathematically via a
brute-force search, not inferred: a real `(connectionSig, client signature)` pair was captured
from a live PRUDP CONNECT attempt. The PRUDP-Lite packet signature is a pure function of
`(accessKey, connectionSig)` — `HMAC-MD5(MD5(accessKey), MD5(accessKey)+connectionSig)` — and
access keys are always exactly 8 hex digits (a 2^32 search space), so brute-forcing all candidates
against that one real captured pair finds the exact key with zero ambiguity.

<details>
<summary>An earlier, incorrect theory (kept for anyone tempted to repeat it on a future title)</summary>

The DNS-resolve hostname the guest asks to resolve when attempting online play
(`g28abaa00-lp1.s.n.srv.nintendo.net`) looked at first like `g<accesskey>-lp1...`, the convention
every other NEX-era title's access key follows — so `28abaa00` was initially logged here as the
confirmed key. It's wrong: checking this org's own deployed ARMS server showed the same hostname
pattern encodes a **Game Server ID**, a separate value from the access key (ARMS's Game Server ID
is `25c08801`, its real access key is `b6b34c51` — entirely different numbers). The hostname alone
never tells you the access key for a NEX title; only a real captured handshake (via the brute-force
method above) does.

</details>

### 2. NEX version and Pia wire shape — CONFIRMED, not guesses

`MPS_NEX_VERSION=40605` (NEX 4.6.5) and `MPS_LEGACY_PIA=0` (Pia 5.33.0, well past the 5.19 that
introduced the modern shape) were both read directly out of the same binary dump's SDK-build
version string table — real measurements, not same-era guesses like the rest of this org's
scaffolded-but-untested titles. Still kept overridable (`MPS_STATION_SCHEME`, `MPS_SECURE_MINOR`)
since nothing about how a Pia 5.33 client actually behaves on the wire has been tested — 5.33 is
newer than any title in this org has connected with so far.

### 3. Ranking / DataStore — unknown whether MPS needs title-specific handling

Party games commonly track minigame/board-game scores. `Ranking` (`0x70`) is registered with the
generic `nex.RankingHandler()`, same as `arms`; if a real client calls it expecting real
leaderboard data rather than a generic ack, this will need the same kind of title-specific stub
`arms` has for `DataStore` (see `arms_stubs.go` for that pattern) — not yet written here since
there's no data yet on whether MPS actually needs it.

## Running

```sh
cp example.env .env    # then edit .env -- MPS_ACCESS_KEY especially
go run .
```

Configuration is entirely through environment variables — see [`example.env`](example.env). No
secrets are baked into the source.

## What this is not

This server ships **no** Nintendo code, keys, or copyrighted assets. It is an independent
reimplementation for use with a community-run replacement service, not affiliated with, endorsed by,
or associated with Nintendo. The NEX access key it uses (once found) is a well-known per-title value
derivable from the game itself, not a secret.

## License

Released under the **[PolyForm Shield License 1.0.0](LICENSE.md)** — source-available: read, use,
modify, and self-host, but do not use it to provide a product that competes with Nextendo Network.
