//! Task persistence plugin — intercepts agent runs to persist task state.
//! Uses adk-rust's plugin system since there is no TaskStore trait.

use std::sync::Arc;

use adk_core::InvocationContext;
use adk_plugin::PluginConfig;
use serde_json::json;
use tracing::info;

use crate::task::KagentTaskStore;

/// Creates a plugin that persists task state after each agent run.
pub struct TaskPersistencePlugin;

impl TaskPersistencePlugin {
    pub fn build(task_store: Arc<KagentTaskStore>) -> PluginConfig {
        let store = task_store.clone();
        PluginConfig {
            name: "kagent-task-persistence".to_string(),
            after_run: Some(Box::new(move |ctx: Arc<dyn InvocationContext>| {
                let store = store.clone();
                Box::pin(async move {
                    // Build a minimal task representation from the invocation context
                    let task = json!({
                        "id": ctx.invocation_id(),
                        "sessionId": ctx.session_id(),
                        "status": {
                            "state": "completed"
                        }
                    });

                    if let Err(e) = store.save(&task).await {
                        tracing::warn!(error = %e, "Failed to persist task after run");
                    } else {
                        info!(task_id = ctx.invocation_id(), "Task persisted");
                    }
                })
            })),
            // All other callbacks are None
            on_user_message: None,
            on_event: None,
            before_run: None,
            before_agent: None,
            after_agent: None,
            before_model: None,
            after_model: None,
            on_model_error: None,
            before_tool: None,
            after_tool: None,
            on_tool_error: None,
            close_fn: None,
        }
    }
}
