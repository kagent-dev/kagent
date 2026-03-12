//! kagent-adk: Rust ADK runtime for kagent.
//!
//! Loads agent configuration from the controller-generated config.json,
//! builds an adk-rust agent, and serves it via A2A protocol.

use std::sync::Arc;

use adk_plugin::{Plugin, PluginManager};
use adk_runner::{Runner, RunnerConfig};
use adk_server::{ServerConfig, create_app_with_a2a};
use adk_session::InMemorySessionService;
use anyhow::{Context, Result};
use clap::Parser;
use tracing::{error, info};

use kagent_adk::a2a::TaskPersistencePlugin;
use kagent_adk::auth::{KagentTokenService, new_http_client_with_token};
use kagent_adk::config::{build_agent, load_agent_configs};
use kagent_adk::session::KagentSessionService;
use kagent_adk::task::KagentTaskStore;

#[derive(Parser)]
#[command(name = "kagent-adk", about = "Kagent Rust ADK runtime")]
struct Args {
    /// Host address to bind to
    #[arg(long, default_value = "0.0.0.0")]
    host: String,

    /// Port to listen on
    #[arg(long, default_value = "8080")]
    port: u16,

    /// Config directory path
    #[arg(long, default_value = "/config")]
    filepath: String,

    /// Log level
    #[arg(long, default_value = "info")]
    log_level: String,
}

#[tokio::main]
async fn main() -> Result<()> {
    let args = Args::parse();

    // Initialize logging
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new(&args.log_level)),
        )
        .json()
        .init();

    // Override from environment variables (matching Go ADK behavior)
    let port = std::env::var("PORT")
        .ok()
        .and_then(|p| p.parse().ok())
        .unwrap_or(args.port);
    let config_dir = std::env::var("CONFIG_DIR").unwrap_or(args.filepath);
    let kagent_url = std::env::var("KAGENT_URL").ok();

    // Load configuration
    let (agent_config, agent_card) = load_agent_configs(&config_dir)?;
    info!(
        config_dir = %config_dir,
        stream = agent_config.get_stream(),
        "Loaded agent configuration"
    );

    // Derive app name from env or agent card
    let app_name = derive_app_name(agent_card.as_ref().map(|c| c.name.as_str()));
    info!(app_name = %app_name, "Derived app name");

    // Set up authenticated HTTP client for kagent persistence
    let token_service = if kagent_url.is_some() {
        let ts = KagentTokenService::new(&app_name);
        match ts.start().await {
            Ok(()) => {
                info!("Token service started");
                Some(ts)
            }
            Err(e) => {
                error!(error = %e, "Failed to start token service, running without persistence");
                None
            }
        }
    } else {
        None
    };

    // Build session service
    let session_service: Arc<dyn adk_session::SessionService> =
        if let (Some(url), Some(ts)) = (&kagent_url, &token_service) {
            let client = new_http_client_with_token(ts)?;
            info!(url = %url, "Using kagent session service");
            Arc::new(KagentSessionService::new(url, client))
        } else {
            info!("Using in-memory session service");
            Arc::new(InMemorySessionService::new())
        };

    // Build agent from config (connects to MCP tool servers)
    let agent = build_agent(&agent_config, &app_name).await?;

    // Build plugin manager with task persistence (if kagent URL is set)
    let plugin_manager = if let (Some(url), Some(ts)) = (&kagent_url, &token_service) {
        let client = new_http_client_with_token(ts)?;
        let task_store = Arc::new(KagentTaskStore::new(url, client));
        let plugin_config = TaskPersistencePlugin::build(task_store);
        let plugin = Plugin::new(plugin_config);
        Some(Arc::new(PluginManager::new(vec![plugin])))
    } else {
        None
    };

    // Build runner config
    let run_config = if agent_config.get_stream() {
        Some(adk_core::RunConfig {
            streaming_mode: adk_core::StreamingMode::SSE,
            ..Default::default()
        })
    } else {
        None
    };

    let runner_config = RunnerConfig {
        app_name: app_name.clone(),
        agent: agent.clone(),
        session_service: session_service.clone(),
        artifact_service: None,
        memory_service: None,
        plugin_manager,
        run_config,
        compaction_config: None,
    };

    let _runner = Runner::new(runner_config).context("failed to create runner")?;

    // Build agent loader for the server
    let agent_loader = Arc::new(adk_core::SingleAgentLoader::new(agent));

    // Build server
    let server_config = ServerConfig::new(agent_loader, session_service)
        .with_security(adk_server::SecurityConfig::development());

    let a2a_base_url = format!("http://{}:{}", args.host, port);
    let app = create_app_with_a2a(server_config, Some(&a2a_base_url));

    // Serve
    let addr = format!("{}:{}", args.host, port);
    info!(addr = %addr, "Starting Rust ADK server");
    let listener = tokio::net::TcpListener::bind(&addr).await?;
    axum::serve(listener, app).await?;

    // Cleanup
    if let Some(ts) = token_service {
        ts.stop();
    }

    Ok(())
}

/// Derives the app name from environment variables or agent card, matching Go ADK behavior.
fn derive_app_name(card_name: Option<&str>) -> String {
    let kagent_name = std::env::var("KAGENT_NAME").ok();
    let kagent_namespace = std::env::var("KAGENT_NAMESPACE").ok();

    if let (Some(namespace), Some(name)) = (kagent_namespace, kagent_name) {
        let ns = namespace.replace('-', "_");
        let n = name.replace('-', "_");
        let app_name = format!("{ns}__NS__{n}");
        info!(
            kagent_namespace = %namespace,
            kagent_name = %name,
            app_name = %app_name,
            "Built app_name from environment variables"
        );
        return app_name;
    }

    if let Some(name) = card_name {
        if !name.is_empty() {
            info!(app_name = %name, "Using agent card name as app_name");
            return name.to_string();
        }
    }

    info!("Using default app_name");
    "rust-adk-agent".to_string()
}
