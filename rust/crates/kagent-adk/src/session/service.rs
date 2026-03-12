//! Kagent session service — persists sessions to the kagent controller HTTP API.
//! Implements adk-rust's `SessionService` trait.

use std::collections::HashMap;

use adk_core::AdkError;
use adk_session::{
    CreateRequest, DeleteRequest, GetRequest, ListRequest, Session, SessionService,
};
use async_trait::async_trait;
use chrono::Utc;
use serde::{Deserialize, Serialize};
use serde_json::Value;
use tracing::{info, warn};

/// Session service backed by the kagent controller's HTTP API.
pub struct KagentSessionService {
    kagent_url: String,
    client: reqwest::Client,
}

impl KagentSessionService {
    pub fn new(kagent_url: &str, client: reqwest::Client) -> Self {
        Self {
            kagent_url: kagent_url.trim_end_matches('/').to_string(),
            client,
        }
    }
}

// ---- HTTP request/response types ----

#[derive(Serialize)]
struct CreateSessionRequest {
    user_id: String,
    agent_ref: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    name: Option<String>,
}

#[derive(Deserialize)]
struct SessionResponse {
    data: SessionData,
}

#[derive(Deserialize)]
struct SessionData {
    id: String,
    user_id: String,
    #[serde(default)]
    events: Vec<Value>,
}

#[derive(Serialize)]
struct AppendEventRequest {
    id: String,
    data: String, // double-JSON-encoded event data
}

// ---- In-memory session impl ----

struct KagentSession {
    id: String,
    app_name: String,
    user_id: String,
    state: HashMap<String, Value>,
    events: Vec<adk_core::Event>,
    last_update: chrono::DateTime<Utc>,
}

impl Session for KagentSession {
    fn id(&self) -> &str {
        &self.id
    }
    fn app_name(&self) -> &str {
        &self.app_name
    }
    fn user_id(&self) -> &str {
        &self.user_id
    }
    fn state(&self) -> &dyn adk_session::State {
        self
    }
    fn events(&self) -> &dyn adk_session::Events {
        self
    }
    fn last_update_time(&self) -> chrono::DateTime<Utc> {
        self.last_update
    }
}

impl adk_session::State for KagentSession {
    fn get(&self, key: &str) -> Option<Value> {
        self.state.get(key).cloned()
    }
    fn set(&mut self, key: String, value: Value) {
        self.state.insert(key, value);
    }
    fn all(&self) -> HashMap<String, Value> {
        self.state.clone()
    }
}

impl adk_session::Events for KagentSession {
    fn all(&self) -> Vec<adk_core::Event> {
        self.events.clone()
    }
    fn len(&self) -> usize {
        self.events.len()
    }
    fn at(&self, index: usize) -> Option<&adk_core::Event> {
        self.events.get(index)
    }
}

// ---- SessionService implementation ----

#[async_trait]
impl SessionService for KagentSessionService {
    async fn create(&self, req: CreateRequest) -> Result<Box<dyn Session>, AdkError> {
        let session_name = extract_session_name(&req.state);
        let body = CreateSessionRequest {
            user_id: req.user_id.clone(),
            agent_ref: req.app_name.clone(),
            id: req.session_id.clone(),
            name: session_name,
        };

        let resp = self
            .client
            .post(format!("{}/api/sessions", self.kagent_url))
            .json(&body)
            .send()
            .await
            .map_err(|e| AdkError::Session(e.to_string()))?;

        let session_resp: SessionResponse = resp
            .error_for_status()
            .map_err(|e| AdkError::Session(e.to_string()))?
            .json()
            .await
            .map_err(|e| AdkError::Session(e.to_string()))?;

        info!(
            session_id = %session_resp.data.id,
            "Created session"
        );

        Ok(Box::new(KagentSession {
            id: session_resp.data.id,
            app_name: req.app_name,
            user_id: session_resp.data.user_id,
            state: req.state,
            events: Vec::new(),
            last_update: Utc::now(),
        }))
    }

    async fn get(&self, req: GetRequest) -> Result<Box<dyn Session>, AdkError> {
        let url = format!(
            "{}/api/sessions/{}?user_id={}&limit=-1",
            self.kagent_url, req.session_id, req.user_id
        );
        let resp = self
            .client
            .get(&url)
            .send()
            .await
            .map_err(|e| AdkError::Session(e.to_string()))?;
        let session_resp: SessionResponse = resp
            .error_for_status()
            .map_err(|e| AdkError::Session(e.to_string()))?
            .json()
            .await
            .map_err(|e| AdkError::Session(e.to_string()))?;

        Ok(Box::new(KagentSession {
            id: session_resp.data.id,
            app_name: req.app_name,
            user_id: session_resp.data.user_id,
            state: HashMap::new(),
            events: Vec::new(),
            last_update: Utc::now(),
        }))
    }

    async fn list(&self, _req: ListRequest) -> Result<Vec<Box<dyn Session>>, AdkError> {
        warn!("list sessions not fully supported — returning empty list");
        Ok(Vec::new())
    }

    async fn delete(&self, req: DeleteRequest) -> Result<(), AdkError> {
        let url = format!(
            "{}/api/sessions/{}?user_id={}",
            self.kagent_url, req.session_id, req.user_id
        );
        self.client
            .delete(&url)
            .send()
            .await
            .map_err(|e| AdkError::Session(e.to_string()))?
            .error_for_status()
            .map_err(|e| AdkError::Session(e.to_string()))?;
        info!(session_id = %req.session_id, "Deleted session");
        Ok(())
    }

    async fn append_event(
        &self,
        session_id: &str,
        event: adk_core::Event,
    ) -> Result<(), AdkError> {
        // Double-JSON-encode the event data to match Go ADK behavior.
        let event_json =
            serde_json::to_string(&event).map_err(|e| AdkError::Session(e.to_string()))?;
        let body = AppendEventRequest {
            id: uuid::Uuid::new_v4().to_string(),
            data: event_json,
        };

        // Use a detached timeout (30s) so client disconnect doesn't interrupt persistence.
        let client = self.client.clone();
        let url = format!(
            "{}/api/sessions/{}/events?user_id=A2A_USER_{}",
            self.kagent_url, session_id, session_id
        );

        tokio::spawn(async move {
            let result = tokio::time::timeout(
                std::time::Duration::from_secs(30),
                client.post(&url).json(&body).send(),
            )
            .await;

            match result {
                Ok(Ok(resp)) => {
                    if let Err(e) = resp.error_for_status() {
                        warn!(error = %e, "Failed to append event to session");
                    }
                }
                Ok(Err(e)) => warn!(error = %e, "HTTP error appending event"),
                Err(_) => warn!("Timeout appending event to session"),
            }
        });

        Ok(())
    }
}

fn extract_session_name(state: &HashMap<String, Value>) -> Option<String> {
    state
        .get("session_name")
        .and_then(|v| v.as_str())
        .map(|s| {
            let truncated: String = s.chars().take(20).collect();
            truncated
        })
}
