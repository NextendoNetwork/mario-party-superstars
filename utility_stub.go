package main

import (
	"fmt"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

// mpsIntegerSettings1 / mpsIntegerSettings2 -- placeholder values, NOT confirmed against a
// real MPS client (no reference server exists for this title, unlike SMB35/kinnay). Ported
// the SHAPE from kinnay/SMB35's source/config.py (same NEX-era title, same generic Utility
// protocol) as a starting point since an empty map is now confirmed wrong for this class of
// title -- our own generic nex.UtilityHandler() was misreading the request entirely (treating
// the request's single index parameter as a list-length, per kinnay's NintendoClients wiki
// Utility-Protocol page: GetIntegerSettings takes ONE Uint32 "integerSettingIndex" and returns
// Map<Uint16,Sint32>, not a List). Confirmed real bug in the shared library, not MPS-specific;
// this stub works around it here pending a proper library fix.
var mpsIntegerSettings1 = map[uint16]int32{
	0: 60, 1: 30, 2: 90, 3: 1, 4: 0, 5: 0, 6: 0, 7: 0,
	8: 0, 9: 0, 10: 5, 11: 3, 12: 1, 13: 30, 14: 30, 15: 180, 16: 0,
}

var mpsIntegerSettings2 = map[uint16]int32{}

// mpsUtilityHandler wraps the generic Utility handler, replacing only GetIntegerSettings
// (and GetStringSettings, answered as an empty-but-CORRECTLY-SHAPED map -- no reference
// values known) with the real index->map protocol shape instead of the generic handler's
// unconditional empty response.
func mpsUtilityHandler() nex.RMCHandler {
	fallback := nex.UtilityHandler()
	return func(conn *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		s := conn.Settings
		switch req.Method {
		case nex.MethodGetIntegerSettings:
			in := nex.NewStreamIn(req.Body, s)
			index := in.U32()
			if in.Err() != nil {
				return nex.NewRMCError(s, nex.ProtocolUtility, req.CallID, nex.ResultCoreInvalidArgument)
			}
			var table map[uint16]int32
			switch index {
			case 0:
				table = mpsIntegerSettings1
			case 10:
				table = mpsIntegerSettings2
			default:
				fmt.Printf("[MPS Utility] GetIntegerSettings unknown index=%d pid=%d\n", index, conn.PID)
				return nex.NewRMCError(s, nex.ProtocolUtility, req.CallID, nex.ResultCoreInvalidArgument)
			}
			out := nex.NewStreamOut(s)
			nex.WriteMap(out, table, func(o *nex.StreamOut, k uint16) { o.U16(k) }, func(o *nex.StreamOut, v int32) { o.S32(v) })
			fmt.Printf("[MPS Utility] GetIntegerSettings index=%d pid=%d -> %d entries\n", index, conn.PID, len(table))
			return nex.NewRMCSuccess(s, nex.ProtocolUtility, req.Method, req.CallID, out.Bytes())

		case nex.MethodGetStringSettings:
			in := nex.NewStreamIn(req.Body, s)
			index := in.U32()
			if in.Err() != nil {
				return nex.NewRMCError(s, nex.ProtocolUtility, req.CallID, nex.ResultCoreInvalidArgument)
			}
			out := nex.NewStreamOut(s)
			nex.WriteMap(out, map[uint16]string{}, func(o *nex.StreamOut, k uint16) { o.U16(k) }, func(o *nex.StreamOut, v string) { o.String(v) })
			fmt.Printf("[MPS Utility] GetStringSettings index=%d pid=%d -> empty (no reference values known)\n", index, conn.PID)
			return nex.NewRMCSuccess(s, nex.ProtocolUtility, req.Method, req.CallID, out.Bytes())

		default:
			return fallback(conn, req)
		}
	}
}
