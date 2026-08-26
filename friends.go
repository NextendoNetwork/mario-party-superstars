package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

// Ported from luigis-mansion-3/friends.go: the real account service's /internal/npln-friends
// (and /internal/presence-batch) route 404s on the production site tonight (confirmed systemic
// -- ARMS, LM3 hit the exact same 404 in their own logs, not an MPS-specific regression), so
// mm.FriendPIDs would otherwise always answer "no friends" and the Join Room screen's method-41
// poll would always target gid=0 -> RendezVousSessionVoid forever, which is exactly what tonight's
// 3-minute "No rooms can be found" loop showed. This file gives MPS the SAME embedded, file-backed
// fallback LM3 already uses for testing, independent of the broken HTTP account backend.

var friendsFilePath = os.Getenv("MPS_FRIENDS_FILE")

const friendCacheTTL = 30 * time.Second

type friendCacheEntry struct {
	pids []uint64
	at   time.Time
}

var (
	friendsCacheMu sync.Mutex
	friendsCache   = map[uint64]friendCacheEntry{}
	friendsWarned  bool
)

type fileFriendCache struct {
	mtime  time.Time
	size   int64
	byPID  map[uint64][]uint64
	loaded bool
}

var (
	fileCacheMu sync.Mutex
	fileCache   fileFriendCache
)

func friendFilePIDs(pid uint64) ([]uint64, bool) {
	fileCacheMu.Lock()
	defer fileCacheMu.Unlock()

	st, err := os.Stat(friendsFilePath)
	if err != nil {
		if !fileCache.loaded {
			friendsWarn("MPS_FRIENDS_FILE %q: %v", friendsFilePath, err)
			fileCache.loaded = true
		}
		return nil, false
	}
	if !fileCache.loaded || !st.ModTime().Equal(fileCache.mtime) || st.Size() != fileCache.size {
		b, err := os.ReadFile(friendsFilePath)
		if err != nil {
			friendsWarn("MPS_FRIENDS_FILE %q: %v", friendsFilePath, err)
			fileCache = fileFriendCache{loaded: true}
			return nil, false
		}
		m := map[string][]uint64{}
		if err := json.Unmarshal(b, &m); err != nil {
			friendsWarn("MPS_FRIENDS_FILE %q: bad JSON: %v", friendsFilePath, err)
			fileCache = fileFriendCache{loaded: true}
			return nil, false
		}
		byPID := make(map[uint64][]uint64, len(m))
		for k, v := range m {
			id, err := strconv.ParseUint(k, 10, 64)
			if err != nil {
				continue
			}
			byPID[id] = v
		}
		fileCache = fileFriendCache{mtime: st.ModTime(), size: st.Size(), byPID: byPID, loaded: true}
	}
	pids, ok := fileCache.byPID[pid]
	return pids, ok
}

// accountFriendPIDs is mm.FriendPIDs: the file-backed source when MPS_FRIENDS_FILE is set, nil
// otherwise (no hardcoded test identities).
func accountFriendPIDs(pid uint64) []uint64 {
	if friendsFilePath == "" {
		return nil
	}
	friendsCacheMu.Lock()
	if e, ok := friendsCache[pid]; ok && time.Since(e.at) < friendCacheTTL {
		friendsCacheMu.Unlock()
		return e.pids
	}
	friendsCacheMu.Unlock()

	pids, _ := friendFilePIDs(pid)

	friendsCacheMu.Lock()
	friendsCache[pid] = friendCacheEntry{pids: pids, at: time.Now()}
	friendsCacheMu.Unlock()
	return pids
}

// dispName already exists in dashboard.go.

func friendsWarn(format string, args ...any) {
	friendsCacheMu.Lock()
	first := !friendsWarned
	friendsWarned = true
	friendsCacheMu.Unlock()
	if first {
		fmt.Printf("[MPS Friends] bridge warning: "+format+"\n", args...)
	}
}

// publishFriendSession pushes a "room open" notification straight over NEX (PublishNotification
// is pure in-process, no dependency on the broken HTTP account backend) to every PID that
// accountFriendPIDs says is this host's friend, right after they create a session.
func publishFriendSession(mm *nex.Matchmaking, pid uint64, gid uint32) {
	types := friendEventTypes()
	mode := os.Getenv("MPS_FRIEND_EVENT_MODE")
	for _, typ := range types {
		ev := &nex.NotificationEvent{PIDSource: pid, Type: typ, Param1: uint64(gid), Param2: 1}
		switch mode {
		case "param2":
			ev.Param1, ev.Param2 = 0, uint64(gid)
		case "str":
			ev.Param1, ev.Param2, ev.StrParam = 0, 1, fmt.Sprintf("%d", gid)
		}
		mm.PublishNotification(pid, typ, ev)
	}
	fmt.Printf("[MPS Friends] auto-published pid=%d gid=%d types=%v mode=%q\n", pid, gid, types, mode)
}

func friendEventTypes() []uint32 {
	raw := os.Getenv("MPS_FRIEND_EVENT_TYPE")
	if raw == "" {
		return []uint32{111000, 128000}
	}
	var out []uint32
	for _, p := range strings.Split(raw, ",") {
		if v, err := strconv.ParseUint(strings.TrimSpace(p), 10, 32); err == nil {
			out = append(out, uint32(v))
		}
	}
	if len(out) == 0 {
		out = []uint32{101}
	}
	return out
}

func registerAccountEndpoints(mux *http.ServeMux) {
	mux.HandleFunc("/internal/npln-friends", accountNplnFriends)
	if friendsFilePath != "" {
		fmt.Printf("[MPS Friends] embedded account endpoints active (friend file: %s)\n", friendsFilePath)
	}
}

func accountGuard(w http.ResponseWriter, r *http.Request) bool {
	if isLoopbackRequest(r) {
		return true
	}
	http.Error(w, "forbidden", http.StatusForbidden)
	return false
}

func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func accountNplnFriends(w http.ResponseWriter, r *http.Request) {
	if !accountGuard(w, r) {
		return
	}
	pid, err := strconv.ParseUint(r.URL.Query().Get("pid"), 10, 64)
	if err != nil || pid == 0 {
		http.Error(w, "bad pid", http.StatusBadRequest)
		return
	}
	pids, ok := friendFilePIDs(pid)
	if !ok {
		http.NotFound(w, r)
		return
	}
	friends := make([]map[string]any, 0, len(pids))
	for _, fpid := range pids {
		friends = append(friends, map[string]any{"pid": fpid, "name": dispName(fpid)})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"pid": pid, "verified": true, "friends": friends})
}
