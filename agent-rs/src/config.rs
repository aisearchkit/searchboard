use anyhow::{Context, Result};
use serde::Deserialize;

#[derive(Debug, Clone, Copy, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum TransportKind {
    Tcp,
    Quic,
    Udp,
}

#[derive(Debug, Clone, Deserialize)]
pub struct ServerCfg {
    /// Searchboard AgentHub address, e.g. "searchboard.example.com:9801".
    pub addr: String,
    /// Telemetry transport. Control + logs always use a reliable stream.
    #[serde(default = "default_transport")]
    pub transport: TransportKind,
    /// Enrollment token (or use mTLS client certs).
    #[serde(default)]
    pub token: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct EsCfg {
    #[serde(default = "default_es_url")]
    pub url: String,
    #[serde(default = "default_interval")]
    pub interval_secs: u64,
}

#[derive(Debug, Clone, Deserialize)]
pub struct LogCfg {
    /// ES log files to tail, e.g. ["/var/log/elasticsearch/*.log"].
    #[serde(default)]
    pub paths: Vec<String>,
    /// Directory for the .offset checkpoint files.
    #[serde(default = "default_state_dir")]
    pub state_dir: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Config {
    pub server: ServerCfg,
    pub es: EsCfg,
    pub logs: LogCfg,
}

fn default_transport() -> TransportKind {
    TransportKind::Tcp
}
fn default_es_url() -> String {
    "http://127.0.0.1:9200".into()
}
fn default_interval() -> u64 {
    10
}
fn default_state_dir() -> String {
    "/var/lib/searchboard-agent".into()
}

impl Config {
    pub fn load(path: &str) -> Result<Self> {
        let text = std::fs::read_to_string(path).with_context(|| format!("read {path}"))?;
        toml::from_str(&text).with_context(|| format!("parse {path}"))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_minimal() {
        let cfg: Config = toml::from_str(
            r#"
            [server]
            addr = "sb:9801"
            [es]
            [logs]
            "#,
        )
        .unwrap();
        assert_eq!(cfg.server.transport, TransportKind::Tcp);
        assert_eq!(cfg.es.url, "http://127.0.0.1:9200");
        assert_eq!(cfg.es.interval_secs, 10);
    }
}
