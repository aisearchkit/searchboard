//! UDP telemetry datagram framing:
//! [Magic 2B][Ver 1B][Type 1B][AgentID 8B][Seq 4B][Flags 1B][CRC32 4B][payload]

use super::{UDP_MAGIC, UDP_VERSION};

pub const HEADER_LEN: usize = 2 + 1 + 1 + 8 + 4 + 1 + 4;

/// Bitwise CRC32 (IEEE) over the payload; small and dependency-free.
pub fn crc32(data: &[u8]) -> u32 {
    let mut crc = 0xFFFF_FFFFu32;
    for &b in data {
        crc ^= b as u32;
        for _ in 0..8 {
            crc = if crc & 1 != 0 { (crc >> 1) ^ 0xEDB8_8320 } else { crc >> 1 };
        }
    }
    !crc
}

pub fn encode(msg_type: u8, agent_id: u64, seq: u32, flags: u8, payload: &[u8]) -> Vec<u8> {
    let mut out = Vec::with_capacity(HEADER_LEN + payload.len());
    out.extend_from_slice(&UDP_MAGIC.to_be_bytes());
    out.push(UDP_VERSION);
    out.push(msg_type);
    out.extend_from_slice(&agent_id.to_be_bytes());
    out.extend_from_slice(&seq.to_be_bytes());
    out.push(flags);
    out.extend_from_slice(&crc32(payload).to_be_bytes());
    out.extend_from_slice(payload);
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn crc_known_vector() {
        assert_eq!(crc32(b"123456789"), 0xCBF4_3926);
    }

    #[test]
    fn header_layout() {
        let pkt = encode(1, 0xAABB, 7, 0, b"hi");
        assert_eq!(pkt.len(), HEADER_LEN + 2);
        assert_eq!(&pkt[0..2], &[0x53, 0x42]);
        assert_eq!(&pkt[HEADER_LEN..], b"hi");
    }
}
