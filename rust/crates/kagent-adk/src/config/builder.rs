//! Builds an adk-rust agent from kagent's AgentConfig.

use std::sync::Arc;

use adk_agent::LlmAgentBuilder;
use adk_core::{Content, Llm, ReadonlyContext, Toolset};
use adk_model::anthropic::{AnthropicClient, AnthropicConfig};
use adk_model::GeminiModel;
use adk_model::ollama::{OllamaConfig, OllamaModel};
use adk_model::openai::{AzureConfig, AzureOpenAIClient, OpenAIClient, OpenAIConfig};
use adk_tool::mcp::McpHttpClientBuilder;
use anyhow::{bail, Context, Result};
use async_trait::async_trait;
use tracing::info;

use super::types::{AgentConfig, ModelConfig};

/// Minimal ReadonlyContext for tool discovery during agent build.
struct ToolDiscoveryContext {
    app_name: String,
    content: Content,
}

#[async_trait]
impl ReadonlyContext for ToolDiscoveryContext {
    fn invocation_id(&self) -> &str { "" }
    fn agent_name(&self) -> &str { &self.app_name }
    fn user_id(&self) -> &str { "" }
    fn app_name(&self) -> &str { &self.app_name }
    fn session_id(&self) -> &str { "" }
    fn branch(&self) -> &str { "" }
    fn user_content(&self) -> &Content { &self.content }
}

/// Constructs an adk-rust Agent from kagent's config.json representation.
/// Connects to all configured MCP tool servers and adds their tools to the agent.
pub async fn build_agent(config: &AgentConfig, app_name: &str) -> Result<Arc<dyn adk_core::Agent>> {
    let model = build_model(&config.model)?;

    let mut builder = LlmAgentBuilder::new(app_name).model(model);

    if !config.instruction.is_empty() {
        builder = builder.instruction(&config.instruction);
    }
    if !config.description.is_empty() {
        builder = builder.description(&config.description);
    }

    // Connect to HTTP MCP servers and add their tools
    for http_tool in &config.http_tools {
        let url = &http_tool.params.url;
        info!(url = %url, "Connecting to HTTP MCP server");

        let mut mcp_builder = McpHttpClientBuilder::new(url);

        // Add headers from config
        for (key, value) in &http_tool.params.headers {
            mcp_builder = mcp_builder.header(key, value);
        }

        // Set timeout if configured
        if let Some(timeout) = http_tool.params.timeout {
            mcp_builder =
                mcp_builder.timeout(std::time::Duration::from_secs_f64(timeout));
        }

        let toolset = mcp_builder
            .connect()
            .await
            .with_context(|| format!("failed to connect to HTTP MCP server at {url}"))?;

        // Apply tool filter if specific tools are listed
        let toolset = if !http_tool.tools.is_empty() {
            let tool_names: Vec<&str> = http_tool.tools.iter().map(|s| s.as_str()).collect();
            toolset.with_tools(&tool_names)
        } else {
            toolset
        };

        // Resolve tools from the toolset and add to builder
        // McpToolset implements Toolset trait; we need a ReadonlyContext to call tools().
        // Use a minimal context just for tool discovery.
        let ctx: Arc<dyn ReadonlyContext> = Arc::new(ToolDiscoveryContext {
            app_name: app_name.to_string(),
            content: Content::new(""),
        });
        let tools = toolset
            .tools(ctx)
            .await
            .with_context(|| format!("failed to list tools from HTTP MCP server at {url}"))?;

        info!(url = %url, count = tools.len(), "Discovered tools from HTTP MCP server");
        for tool in tools {
            builder = builder.tool(tool);
        }

        // Mark require_approval tools
        for tool_name in &http_tool.require_approval {
            builder = builder.require_tool_confirmation(tool_name);
        }
    }

    // Connect to SSE MCP servers and add their tools
    // SSE servers use the same streamable HTTP transport (which handles SSE fallback per MCP spec)
    for sse_tool in &config.sse_tools {
        let url = &sse_tool.params.url;
        info!(url = %url, "Connecting to SSE MCP server");

        let mut mcp_builder = McpHttpClientBuilder::new(url);

        for (key, value) in &sse_tool.params.headers {
            mcp_builder = mcp_builder.header(key, value);
        }

        if let Some(timeout) = sse_tool.params.timeout {
            mcp_builder =
                mcp_builder.timeout(std::time::Duration::from_secs_f64(timeout));
        }

        let toolset = mcp_builder
            .connect()
            .await
            .with_context(|| format!("failed to connect to SSE MCP server at {url}"))?;

        let toolset = if !sse_tool.tools.is_empty() {
            let tool_names: Vec<&str> = sse_tool.tools.iter().map(|s| s.as_str()).collect();
            toolset.with_tools(&tool_names)
        } else {
            toolset
        };

        let ctx: Arc<dyn ReadonlyContext> = Arc::new(ToolDiscoveryContext {
            app_name: app_name.to_string(),
            content: Content::new(""),
        });
        let tools = toolset
            .tools(ctx)
            .await
            .with_context(|| format!("failed to list tools from SSE MCP server at {url}"))?;

        info!(url = %url, count = tools.len(), "Discovered tools from SSE MCP server");
        for tool in tools {
            builder = builder.tool(tool);
        }

        for tool_name in &sse_tool.require_approval {
            builder = builder.require_tool_confirmation(tool_name);
        }
    }

    let agent = builder
        .build()
        .context("failed to build LLM agent from config")?;

    info!(name = app_name, "Built agent from config");
    Ok(Arc::new(agent))
}

/// Dispatches model construction based on the config type tag.
fn build_model(config: &ModelConfig) -> Result<Arc<dyn Llm>> {
    match config {
        ModelConfig::OpenAI(c) => {
            let api_key = extract_api_key(&c.base.headers)?;
            if c.base_url.is_empty() {
                let cfg = OpenAIConfig::new(&api_key, &c.base.model);
                let client = OpenAIClient::new(cfg).context("failed to create OpenAI client")?;
                Ok(Arc::new(client))
            } else {
                let client = OpenAIClient::compatible(&api_key, &c.base_url, &c.base.model)
                    .context("failed to create OpenAI-compatible client")?;
                Ok(Arc::new(client))
            }
        }
        ModelConfig::AzureOpenAI(c) => {
            let api_key = extract_api_key(&c.base.headers)?;
            let cfg = AzureConfig::new(
                &api_key,
                "",
                "2024-02-01",
                &c.base.model,
            );
            let client =
                AzureOpenAIClient::new(cfg).context("failed to create Azure OpenAI client")?;
            Ok(Arc::new(client))
        }
        ModelConfig::Anthropic(c) => {
            let api_key = extract_api_key(&c.base.headers)?;
            let mut cfg = AnthropicConfig::new(&api_key, &c.base.model);
            if let Some(max_tokens) = c.max_tokens {
                cfg = cfg.with_max_tokens(max_tokens as u32);
            }
            if !c.base_url.is_empty() {
                cfg = cfg.with_base_url(&c.base_url);
            }
            let client =
                AnthropicClient::new(cfg).context("failed to create Anthropic client")?;
            Ok(Arc::new(client))
        }
        ModelConfig::Gemini(c) => {
            let api_key = extract_api_key(&c.base.headers)?;
            let model =
                GeminiModel::new(&api_key, &c.base.model).context("failed to create Gemini model")?;
            Ok(Arc::new(model))
        }
        ModelConfig::GeminiVertexAI(c) => {
            let api_key = extract_api_key(&c.base.headers).unwrap_or_default();
            let model = GeminiModel::new(&api_key, &c.base.model)
                .context("failed to create Gemini Vertex AI model")?;
            Ok(Arc::new(model))
        }
        ModelConfig::GeminiAnthropic(c) => {
            let api_key = extract_api_key(&c.base.headers).unwrap_or_default();
            let cfg = AnthropicConfig::new(&api_key, &c.base.model);
            let client =
                AnthropicClient::new(cfg).context("failed to create Gemini Anthropic client")?;
            Ok(Arc::new(client))
        }
        ModelConfig::Ollama(c) => {
            let cfg = OllamaConfig::new(&c.base.model);
            let client = OllamaModel::new(cfg).context("failed to create Ollama model")?;
            Ok(Arc::new(client))
        }
        ModelConfig::Bedrock(c) => {
            bail!(
                "AWS Bedrock model type is not yet supported by the Rust ADK runtime. \
                 Model '{}' in region '{}' cannot be used. \
                 Consider using the Python or Go ADK runtime for Bedrock support.",
                c.base.model,
                c.region
            )
        }
    }
}

/// Extracts the API key from the Authorization header (Bearer token).
fn extract_api_key(headers: &std::collections::HashMap<String, String>) -> Result<String> {
    if let Some(auth) = headers.get("Authorization").or(headers.get("authorization")) {
        if let Some(token) = auth.strip_prefix("Bearer ") {
            return Ok(token.to_string());
        }
        return Ok(auth.clone());
    }
    // Check for x-api-key header (some providers use this)
    if let Some(key) = headers.get("x-api-key") {
        return Ok(key.clone());
    }
    // Fall back to env vars
    for var in [
        "OPENAI_API_KEY",
        "ANTHROPIC_API_KEY",
        "GOOGLE_API_KEY",
        "GEMINI_API_KEY",
    ] {
        if let Ok(key) = std::env::var(var) {
            if !key.is_empty() {
                return Ok(key);
            }
        }
    }
    bail!("no API key found in headers or environment variables")
}
