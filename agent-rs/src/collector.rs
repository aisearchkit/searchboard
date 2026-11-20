//! Local metrics collection: ES `_nodes/_local/stats` over loopback plus /proc.
//! TODO: HTTP client for 127.0.0.1:9200, filter_path trimming, shard-level aggregation.

use anyhow::Result;
use serde::Serialize;

#[derive(Debug, Default, Serialize)]
pub struct ProcStats {
    pub load1: f64,
    pub mem_available_kb: u64,
}

/// Parse the first field of /proc/loadavg and MemAvailable from /proc/meminfo.
pub fn read_proc() -> Result<ProcStats> {
    let load = std::fs::read_to_string("/proc/loadavg")?;
    let load1 = load
        .split_whitespace()
        .next()
        .and_then(|s| s.parse().ok())
        .unwrap_or(0.0);
    let mem = std::fs::read_to_string("/proc/meminfo")?;
    let mem_available_kb = mem
        .lines()
        .find(|l| l.starts_with("MemAvailable:"))
        .and_then(|l| l.split_whitespace().nth(1))
        .and_then(|v| v.parse().ok())
        .unwrap_or(0);
    Ok(ProcStats {
        load1,
        mem_available_kb,
    })
}

#[cfg(test)]
mod tests {
    #[test]
    fn reads_proc() {
        let s = super::read_proc().unwrap();
        assert!(s.mem_available_kb > 0);
    }
}
