//! BYO (Bring Your Own) agent example.
//!
//! Demonstrates how to build a custom Rust agent using kagent-adk as a library.
//! Users can define their own agents programmatically while still getting
//! kagent's session persistence, token auth, and A2A server.

use std::sync::Arc;

use adk_agent::LlmAgentBuilder;
use adk_model::openai::{OpenAIClient, OpenAIConfig};
use adk_server::{ServerConfig, create_app_with_a2a};
use adk_session::InMemorySessionService;
use anyhow::Result;

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt::init();

    let api_key =
        std::env::var("OPENAI_API_KEY").expect("OPENAI_API_KEY environment variable required");

    // Build model
    let model_config = OpenAIConfig::new(&api_key, "gpt-4o-mini");
    let model = OpenAIClient::new(model_config)?;

    // Build agent
    let agent = LlmAgentBuilder::new("byo-rust-agent")
        .description("A custom BYO Rust agent")
        .instruction("You are a helpful assistant built with kagent's Rust ADK.")
        .model(Arc::new(model))
        .build()?;

    // Set up server with in-memory sessions (BYO agents can also use KagentSessionService)
    let session_service = Arc::new(InMemorySessionService::new());
    let agent_loader = Arc::new(adk_core::SingleAgentLoader::new(Arc::new(agent)));

    let server_config = ServerConfig::new(agent_loader, session_service)
        .with_security(adk_server::SecurityConfig::development());

    let app = create_app_with_a2a(server_config, Some("http://0.0.0.0:8080"));

    println!("BYO Rust agent listening on http://0.0.0.0:8080");
    let listener = tokio::net::TcpListener::bind("0.0.0.0:8080").await?;
    axum::serve(listener, app).await?;

    Ok(())
}
