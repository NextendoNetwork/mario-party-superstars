package main

import (
	"testing"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

// 0x29 must reach the single-gathering lookup instead of the not-implemented fallback,
// which is what made clients drop the connection roughly two seconds later.
func TestMPSFindMatchmakeSessionByGatheringIDDetailIsRouted(t *testing.T) {
	s := nex.NewSwitchSettings("e915510f", 40605)
	mm := nex.NewMatchmaking()
	conn := &nex.Connection{Settings: s, PID: 1800000001}
	body := []byte{0x39, 0x30, 0x00, 0x00} // gid 12345, absent from the store

	got := mpsExtensionHandler(mm)(conn, nex.NewRMCRequest(s, nex.ProtocolMatchmakeExtension,
		methodFindMatchmakeSessionByGatheringIDDetail, 7, body))
	if got == nil {
		t.Fatal("no response")
	}
	if !got.IsError {
		t.Fatalf("absent gathering should error, got %+v", got)
	}

	// Same request through the library's identically shaped 0x31 -- the outcome must match,
	// proving 0x29 landed on that path rather than on notImplemented.
	want := mm.ExtensionHandler()(conn, nex.NewRMCRequest(s, nex.ProtocolMatchmakeExtension,
		nex.MethodFindMatchmakeSessionBySingleID, 7, body))
	if got.Result != want.Result {
		t.Errorf("result %#x, want %#x (0x31's result)", got.Result, want.Result)
	}
	if got.CallID != 7 {
		t.Errorf("call id %d, want 7", got.CallID)
	}

	notImpl := mm.ExtensionHandler()(conn, nex.NewRMCRequest(s, nex.ProtocolMatchmakeExtension,
		0x7E, 7, body))
	if got.Result == notImpl.Result {
		t.Errorf("0x29 still answered not-implemented (%#x)", got.Result)
	}
}
