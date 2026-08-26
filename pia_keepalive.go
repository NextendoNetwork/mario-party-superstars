package main

import (
	"fmt"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

// piaKeepalivePacketType is the undocumented PRUDP packet type 8 that Pia sends as a periodic
// keepalive on the secure stream (empty payload, NeedsAck, ~every 3s). The base PRUDP switch
// only knows types 0-4, so with no handler the keepalive goes un-ACKed and the console treats
// the connection as dead -- reconnecting from scratch every few seconds instead of proceeding.
// SSBU (also NEX 4.6+, Pia 5.19) hit this exact bug; MPS's own retry loop (a full reconnect
// roughly every ~3-4s, right after Register+GetIntegerSettings, matching this keepalive's
// period exactly) is consistent with the same cause. ACKing it is the proven fix there.
const piaKeepalivePacketType uint8 = 8

func setupMPSType8Keepalive(endpoint *nex.Endpoint) {
	endpoint.RegisterCustomPacketHandler(piaKeepalivePacketType, func(c *nex.Connection, p *nex.Packet) {
		if p.HasFlag(nex.FlagNeedACK) {
			c.SendAck(p)
		}
		fmt.Printf("[MPS Type8] Pia keepalive seqID=%d -> ACK\n", p.PacketID)
	})
}
