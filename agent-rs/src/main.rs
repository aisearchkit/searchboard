//! Searchboard node agent.
//!
//! Runs next to an ES/OpenSearch node, collects local metrics (127.0.0.1:9200 + /proc)
//! and log events, and *dials out* to the Searchboard server: no inbound port is needed.

// The agent modules are implemented ahead of the runtime loop that wires them together.
// Remove this allowance once `main` spawns the collector, tailer and transport tasks.
#![allow(dead_code)]

mod collector;
mod config;
mod logtail;
mod ring;
mod transport;

use anyhow::Result;

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(tracing_subscriber::EnvFilter::from_default_env())
        .init();

    let path = std::env::args()
        .nth(1)
        .unwrap_or_else(|| "agent.toml".to_string());
    let cfg = config::Config::load(&path)?;
    tracing::info!(server = %cfg.server.addr, transport = ?cfg.server.transport, "searchboard-agent starting");

    // TODO: spawn collector loop, log tailer, and transport sender; wait for ctrl-c.
    tokio::signal::ctrl_c().await?;
    Ok(())
}
