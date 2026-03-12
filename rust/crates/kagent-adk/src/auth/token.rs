//! Kubernetes service account token management.
//! Reads the projected token from the well-known path and refreshes it periodically.

use std::sync::{Arc, RwLock};
use std::time::Duration;

use anyhow::{Context, Result};
use reqwest::header::{HeaderMap, HeaderValue, AUTHORIZATION};
use tracing::{error, info};

const TOKEN_PATH: &str = "/var/run/secrets/tokens/kagent-token";
const REFRESH_INTERVAL: Duration = Duration::from_secs(60);

/// Manages the K8s service account token lifecycle.
#[derive(Clone)]
pub struct KagentTokenService {
    token: Arc<RwLock<String>>,
    app_name: String,
    shutdown: Arc<tokio::sync::Notify>,
}

impl KagentTokenService {
    pub fn new(app_name: &str) -> Self {
        Self {
            token: Arc::new(RwLock::new(String::new())),
            app_name: app_name.to_string(),
            shutdown: Arc::new(tokio::sync::Notify::new()),
        }
    }

    /// Reads the initial token and starts the background refresh loop.
    pub async fn start(&self) -> Result<()> {
        self.refresh_token()?;
        info!("Token service started");

        let token = self.token.clone();
        let shutdown = self.shutdown.clone();
        tokio::spawn(async move {
            let mut interval = tokio::time::interval(REFRESH_INTERVAL);
            loop {
                tokio::select! {
                    _ = interval.tick() => {
                        match std::fs::read_to_string(TOKEN_PATH) {
                            Ok(t) => {
                                let trimmed = t.trim().to_string();
                                if let Ok(mut guard) = token.write() {
                                    *guard = trimmed;
                                }
                            }
                            Err(e) => error!(error = %e, "Failed to refresh token"),
                        }
                    }
                    _ = shutdown.notified() => {
                        info!("Token refresh loop stopped");
                        return;
                    }
                }
            }
        });

        Ok(())
    }

    pub fn stop(&self) {
        self.shutdown.notify_one();
    }

    /// Returns the current token value.
    pub fn get_token(&self) -> String {
        self.token.read().map(|t| t.clone()).unwrap_or_default()
    }

    /// Returns headers with the current token and agent name.
    pub fn auth_headers(&self) -> HeaderMap {
        let mut headers = HeaderMap::new();
        let token = self.get_token();
        if !token.is_empty() {
            if let Ok(val) = HeaderValue::from_str(&format!("Bearer {token}")) {
                headers.insert(AUTHORIZATION, val);
            }
        }
        if let Ok(val) = HeaderValue::from_str(&self.app_name) {
            headers.insert("X-Agent-Name", val);
        }
        headers
    }

    fn refresh_token(&self) -> Result<()> {
        let token_str = std::fs::read_to_string(TOKEN_PATH)
            .with_context(|| format!("failed to read token from {TOKEN_PATH}"))?;
        let trimmed = token_str.trim().to_string();
        let mut guard = self
            .token
            .write()
            .map_err(|e| anyhow::anyhow!("token lock poisoned: {e}"))?;
        *guard = trimmed;
        Ok(())
    }
}

/// Creates a reqwest Client with default auth headers from the token service.
pub fn new_http_client_with_token(token_service: &KagentTokenService) -> Result<reqwest::Client> {
    let headers = token_service.auth_headers();
    reqwest::Client::builder()
        .default_headers(headers)
        .timeout(Duration::from_secs(30))
        .build()
        .context("failed to build HTTP client")
}
