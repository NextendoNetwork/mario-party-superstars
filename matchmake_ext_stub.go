package main

import (
	nex "github.com/NextendoNetwork/nextendo-nex"
)

// FindMatchmakeSessionByGatheringIdDetail (0x6D/0x29). MPS calls it after creating or
// joining a room; the library only implements the identically shaped 0x31
// (FindMatchmakeSessionBySingleGatheringId) -- both take a Uint32 gid and return a
// MatchmakeSession -- so the request is delegated and the method id restored on the way
// back. Left unhandled, the client dropped the connection about two seconds later.
const methodFindMatchmakeSessionByGatheringIDDetail uint32 = 0x29

func mpsExtensionHandler(mm *nex.Matchmaking) nex.RMCHandler {
	// MPS publishes the room settings right after creating the session, and carries the
	// on-screen room code in Attributes[2] rather than Codeword. The library's default
	// acknowledges that update without storing it, so joiners read back a session with
	// neither the code nor the room state.
	mm.SessionPartPersists = true
	fallback := mm.ExtensionHandler()
	return func(conn *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		if req.Method != methodFindMatchmakeSessionByGatheringIDDetail {
			return fallback(conn, req)
		}
		delegated := nex.NewRMCRequest(conn.Settings, nex.ProtocolMatchmakeExtension,
			nex.MethodFindMatchmakeSessionBySingleID, req.CallID, req.Body)
		resp := fallback(conn, delegated)
		if resp != nil && !resp.IsError {
			resp.Method = req.Method
		}
		return resp
	}
}
