//! Loads and validates agent configuration from the config directory.

use std::path::Path;

use anyhow::{Context, Result};
use tracing::{info, warn};

use super::types::{AgentCard, AgentConfig};

/// Loads agent configuration from the given directory.
/// Reads `config.json` (required) and `agent-card.json` (optional).
pub fn load_agent_configs(config_dir: &str) -> Result<(AgentConfig, Option<AgentCard>)> {
    let dir = Path::new(config_dir);

    // Load config.json (required)
    let config_path = dir.join("config.json");
    let config_data = std::fs::read_to_string(&config_path)
        .with_context(|| format!("failed to read config file: {}", config_path.display()))?;
    let config: AgentConfig = serde_json::from_str(&config_data)
        .with_context(|| "failed to parse config.json")?;

    // Validate
    validate_config(&config)?;

    // Load agent-card.json (optional)
    let card_path = dir.join("agent-card.json");
    let agent_card = match std::fs::read_to_string(&card_path) {
        Ok(data) => match serde_json::from_str::<AgentCard>(&data) {
            Ok(card) => {
                info!(name = %card.name, "Loaded agent card");
                Some(card)
            }
            Err(e) => {
                warn!(error = %e, "Failed to parse agent-card.json, ignoring");
                None
            }
        },
        Err(_) => {
            info!("No agent-card.json found, using defaults");
            None
        }
    };

    Ok((config, agent_card))
}

fn validate_config(config: &AgentConfig) -> Result<()> {
    if config.instruction.is_empty() {
        warn!("Agent instruction is empty — agent may not behave as expected");
    }

    // Validate tool URLs
    for (i, tool) in config.http_tools.iter().enumerate() {
        anyhow::ensure!(
            !tool.params.url.is_empty(),
            "http_tools[{i}].params.url is required"
        );
    }
    for (i, tool) in config.sse_tools.iter().enumerate() {
        anyhow::ensure!(
            !tool.params.url.is_empty(),
            "sse_tools[{i}].params.url is required"
        );
    }
    for (i, agent) in config.remote_agents.iter().enumerate() {
        anyhow::ensure!(
            !agent.name.is_empty() && !agent.url.is_empty(),
            "remote_agents[{i}] requires both name and url"
        );
    }

    info!(
        model_type = ?std::mem::discriminant(&config.model),
        stream = config.get_stream(),
        http_tools = config.http_tools.len(),
        sse_tools = config.sse_tools.len(),
        remote_agents = config.remote_agents.len(),
        "Agent configuration validated"
    );

    Ok(())
}
