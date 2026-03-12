//! Task persistence — stores A2A task results to the kagent controller HTTP API.
//! Since adk-rust doesn't expose a TaskStore trait, we use the plugin system
//! to intercept events after each run and persist task state.

use anyhow::Result;
use serde_json::Value;
use tracing::{info, warn};

/// Persists A2A tasks to the kagent controller's /api/tasks endpoint.
pub struct KagentTaskStore {
    kagent_url: String,
    client: reqwest::Client,
}

impl KagentTaskStore {
    pub fn new(kagent_url: &str, client: reqwest::Client) -> Self {
        Self {
            kagent_url: kagent_url.trim_end_matches('/').to_string(),
            client,
        }
    }

    /// Saves a task to the kagent controller.
    /// Strips partial streaming events before persistence.
    pub async fn save(&self, task: &Value) -> Result<()> {
        let cleaned = strip_partial_events(task);

        let resp = self
            .client
            .post(format!("{}/api/tasks", self.kagent_url))
            .json(&cleaned)
            .send()
            .await?;

        if let Err(e) = resp.error_for_status() {
            warn!(error = %e, "Failed to save task");
            return Err(e.into());
        }

        info!("Task saved successfully");
        Ok(())
    }

    /// Retrieves a task from the kagent controller.
    pub async fn get(&self, task_id: &str) -> Result<Value> {
        let resp = self
            .client
            .get(format!("{}/api/tasks/{}", self.kagent_url, task_id))
            .send()
            .await?
            .error_for_status()?;

        let body: StandardResponse = resp.json().await?;
        Ok(body.data)
    }
}

#[derive(serde::Deserialize)]
struct StandardResponse {
    data: Value,
}

/// Strips events marked with `adk_partial` or `kagent_adk_partial` metadata.
/// These are streaming intermediates that shouldn't be in the final history.
fn strip_partial_events(task: &Value) -> Value {
    let mut task = task.clone();

    // Strip partial artifacts
    if let Some(artifacts) = task.get_mut("artifacts").and_then(|a| a.as_array_mut()) {
        artifacts.retain(|artifact| !is_partial(artifact));
    }

    // Strip partial history messages
    if let Some(history) = task.get_mut("history").and_then(|h| h.as_array_mut()) {
        history.retain(|msg| !is_partial(msg));
    }

    task
}

fn is_partial(value: &Value) -> bool {
    if let Some(metadata) = value.get("metadata").and_then(|m| m.as_object()) {
        return metadata.contains_key("adk_partial") || metadata.contains_key("kagent_adk_partial");
    }
    false
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_strip_partial_events() {
        let task = json!({
            "id": "task-1",
            "artifacts": [
                {"index": 0, "metadata": {"adk_partial": true}},
                {"index": 1, "parts": [{"type": "text", "text": "final"}]}
            ],
            "history": [
                {"role": "agent", "metadata": {"kagent_adk_partial": true}},
                {"role": "agent", "parts": [{"type": "text", "text": "done"}]}
            ]
        });

        let cleaned = strip_partial_events(&task);
        assert_eq!(cleaned["artifacts"].as_array().unwrap().len(), 1);
        assert_eq!(cleaned["history"].as_array().unwrap().len(), 1);
    }
}
