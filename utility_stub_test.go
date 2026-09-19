package main

import (
	"encoding/binary"
	nex "github.com/NextendoNetwork/nextendo-nex"
	"testing"
)

// Exercise the actual response encoding consumed by MPS, independent of map order.
func TestMPSUtilityRTTDefaultsOnWire(t *testing.T) {
	s := nex.NewSwitchSettings("e915510f", 40605)
	conn := &nex.Connection{Settings: s, PID: 1}
	req := nex.NewRMCRequest(s, nex.ProtocolUtility, nex.MethodGetIntegerSettings, 42, []byte{0, 0, 0, 0})
	resp := mpsUtilityHandler()(conn, req)
	if resp == nil || resp.IsError || resp.CallID != 42 {
		t.Fatalf("utility response: %+v", resp)
	}
	b := resp.Body
	if len(b) < 4 {
		t.Fatal("missing map length")
	}
	count := int(binary.LittleEndian.Uint32(b))
	if len(b) != 4+count*6 {
		t.Fatalf("invalid Uint16/Sint32 map length %d for %d entries", len(b), count)
	}
	values := map[uint16]int32{}
	for i := 0; i < count; i++ {
		entry := b[4+i*6:]
		key := binary.LittleEndian.Uint16(entry)
		if _, exists := values[key]; exists {
			t.Fatalf("duplicate setting %d", key)
		}
		values[key] = int32(binary.LittleEndian.Uint32(entry[2:]))
	}
	for key, want := range map[uint16]int32{0: 1000, 1: 1000, 2: 500, 3: 250} {
		if got, ok := values[key]; !ok || got != want {
			t.Errorf("MPS RTT setting %d = %d (present %v), want client default %d ms", key, got, ok, want)
		}
	}
}
