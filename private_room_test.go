package main

import (
	nex "github.com/NextendoNetwork/nextendo-nex"
	"testing"
)

func TestMPSPrivateRoomUpdateSurvivesDetailLookup(t *testing.T) {
	s := nex.NewSwitchSettings("e915510f", 40605)
	ep := nex.NewEndpoint(s)
	host := &nex.Connection{Settings: s, Endpoint: ep, PID: 1001}
	joiner := &nex.Connection{Settings: s, Endpoint: ep, PID: 1002}
	mm := nex.NewMatchmaking()
	handler := mpsExtensionHandler(mm)
	call := func(c *nex.Connection, method uint32, body []byte) *nex.RMCMessage {
		r := handler(c, nex.NewRMCRequest(s, nex.ProtocolMatchmakeExtension, method, 1, body))
		if r == nil || r.IsError {
			t.Fatalf("method %d failed: %+v", method, r)
		}
		return r
	}
	p := nex.AutoMatchmakeParam{Session: nex.MatchmakeSession{OpenParticipation: true}}
	p.Session.MaxParticipants = 4
	out := nex.NewStreamOut(s)
	out.Add(&p)
	created := call(host, nex.MethodCreateMatchmakeSessionWithParam, out.Bytes())
	var session nex.MatchmakeSession
	in := nex.NewStreamIn(created.Body, s)
	in.Extract(&session)
	if in.Err() != nil {
		t.Fatal(in.Err())
	}
	update := nex.UpdateMatchmakeSessionParam{GID: session.ID, ModificationFlags: 0x4002, Codeword: "797146", OpenParticipation: true, Attributes: []uint32{1, 2, 3, 4, 5, 6}}
	out = nex.NewStreamOut(s)
	out.Add(&update)
	call(host, nex.MethodUpdateMatchmakeSessionPart, out.Bytes())
	out = nex.NewStreamOut(s)
	out.U32(session.ID)
	detail := call(joiner, methodFindMatchmakeSessionByGatheringIDDetail, out.Bytes())
	var got nex.MatchmakeSession
	in = nex.NewStreamIn(detail.Body, s)
	in.Extract(&got)
	if in.Err() != nil {
		t.Fatal(in.Err())
	}
	if got.Codeword != update.Codeword {
		t.Fatalf("host update acknowledged but room code lost: %q", got.Codeword)
	}
	if len(got.Attribs) != 6 || got.Attribs[5] != 6 {
		t.Fatalf("host attributes lost: %v", got.Attribs)
	}
}
