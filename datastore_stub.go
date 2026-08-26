package main

// MPS DataStore (0x73) stub.
//
// SSBU (also NEX 4.6+, also using ValidateAndRequestTicketWithParam like MPS) called a set of
// DataStore getters during online bring-up and soft-locked -- reconnecting from scratch instead
// of proceeding -- whenever DataStore had no registered handler at all and calls to it got
// silently dropped. The fix there was a minimal stub: an empty list for most methods, and
// DataStore::NotFound (0x80690004) specifically for method 8 (GetMetaMultiple), which the game
// handles gracefully as "nothing saved yet" rather than an error. MPS never had ANY DataStore
// handler registered -- not even NotImplemented -- which is a strictly worse gap than what broke
// SSBU. Registering this stub is the same fix, applied before ever seeing an MPS-specific
// DataStore call logged (the connection currently never survives long enough to make one).

import (
	"fmt"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

const protocolDataStore uint16 = 0x73

// dataStoreStubHandler answers every DataStore method with a minimal valid response, same
// shape as the SSBU fix: an empty list (count = 0) for most methods, NotFound for method 8.
func dataStoreStubHandler() nex.RMCHandler {
	return func(conn *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		s := conn.Settings

		if req.Method == 8 {
			fmt.Printf("[MPS DataStore] 0x73.8 -> NotFound 0x80690004\n")
			return nex.NewRMCError(s, protocolDataStore, req.CallID, 0x80690004)
		}

		out := nex.NewStreamOut(s)
		out.U32(0) // empty list / count = 0
		fmt.Printf("[MPS DataStore] 0x73.%d callID=%d -> empty list (stub)\n", req.Method, req.CallID)
		return nex.NewRMCSuccess(s, protocolDataStore, req.Method, req.CallID, out.Bytes())
	}
}
