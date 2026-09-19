package main

import (
	"fmt"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

// Utility index 0 keys 0..3 are MPS RTT limits in milliseconds, not the
// unrelated SMB35 settings previously used here. MPS 1.1.1 loads these in
// FUN_001508d0; hs::Net::ResetRttParameter (0x0014ff44, analysis image base
// 0x100000) supplies the defaults below. Key 0 is the disconnect/warning
// cutoff: FUN_00151324 leaves after every remote station exceeds it for
// five seconds. Keys 1, 2, 3 are the descending signal-bar thresholds.
// The old 60 ms cutoff disconnected healthy WAN sessions while LAN worked.
// Keys 4..16 remain the existing placeholders; their values are not verified.
var mpsIntegerSettings1 = map[uint16]int32{
	0: 1000, 1: 1000, 2: 500, 3: 250, 4: 0, 5: 0, 6: 0, 7: 0,
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
