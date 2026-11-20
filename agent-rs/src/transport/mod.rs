//! Transports. Control + logs: reliable stream (TCP+TLS, or QUIC stream).
//! Telemetry: tcp | quic | udp (configurable, pushed down by the server).

pub mod udp;

/// UDP telemetry header (see server agenthub): magic "SB".
pub const UDP_MAGIC: u16 = 0x5342;
pub const UDP_VERSION: u8 = 1;
// TODO: TCP+TLS framed sender (tokio-rustls), reconnect w/ backoff, token/mTLS auth,
//       config hot-update handler; QUIC sender behind the `quic` feature.
