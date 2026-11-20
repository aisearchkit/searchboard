// Package agenthub accepts reverse connections from Rust agents
// (TCP+TLS by default; QUIC / UDP telemetry optional) and decodes frames.
package agenthub

import "context"

type Transport string

const (
	TransportTCP  Transport = "tcp"
	TransportQUIC Transport = "quic"
	TransportUDP  Transport = "udp"
)

// UDP telemetry datagram header (design doc section 5.2):
// [Magic 2B][Ver 1B][Type 1B][AgentID 8B][Seq 4B][Flags 1B][CRC32 4B][Protobuf]
const (
	UDPMagic      uint16 = 0x5342 // "SB"
	UDPHeaderSize        = 2 + 1 + 1 + 8 + 4 + 1 + 4
)

// Hub is the ingress gateway for agents.
type Hub interface {
	Serve(ctx context.Context) error
}

// TODO: implement TCP+TLS framing, auth (mTLS/token), config push, QUIC, UDP.
