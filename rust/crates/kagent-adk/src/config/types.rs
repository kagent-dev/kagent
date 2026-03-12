//! Config types matching the controller-generated config.json schema.
//! These must stay in sync with `go/api/adk/types.go`.

use std::collections::HashMap;

use serde::Deserialize;
use serde_json::Value;

/// Top-level agent configuration, deserialized from config.json.
#[derive(Debug, Deserialize)]
pub struct AgentConfig {
    pub model: ModelConfig,
    #[serde(default)]
    pub description: String,
    #[serde(default)]
    pub instruction: String,
    #[serde(default)]
    pub http_tools: Vec<HttpMcpServerConfig>,
    #[serde(default)]
    pub sse_tools: Vec<SseMcpServerConfig>,
    #[serde(default)]
    pub remote_agents: Vec<RemoteAgentConfig>,
    pub execute_code: Option<bool>,
    pub stream: Option<bool>,
    pub memory: Option<MemoryConfig>,
    pub context_config: Option<AgentContextConfig>,
}

impl AgentConfig {
    pub fn get_stream(&self) -> bool {
        self.stream.unwrap_or(false)
    }
}

// ---------- Model types ----------

/// Model configuration, dispatched by the `type` field.
#[derive(Debug, Deserialize)]
#[serde(tag = "type")]
pub enum ModelConfig {
    #[serde(rename = "openai")]
    OpenAI(OpenAIModelConfig),
    #[serde(rename = "azure_openai")]
    AzureOpenAI(AzureOpenAIModelConfig),
    #[serde(rename = "anthropic")]
    Anthropic(AnthropicModelConfig),
    #[serde(rename = "gemini")]
    Gemini(GeminiModelConfig),
    #[serde(rename = "gemini_vertex_ai")]
    GeminiVertexAI(GeminiVertexAIModelConfig),
    #[serde(rename = "gemini_anthropic")]
    GeminiAnthropic(GeminiAnthropicModelConfig),
    #[serde(rename = "ollama")]
    Ollama(OllamaModelConfig),
    #[serde(rename = "bedrock")]
    Bedrock(BedrockModelConfig),
}

/// Fields shared by all model types.
#[derive(Debug, Deserialize, Default)]
pub struct BaseModelConfig {
    pub model: String,
    #[serde(default)]
    pub headers: HashMap<String, String>,
    pub tls_insecure_skip_verify: Option<bool>,
    pub tls_ca_cert_path: Option<String>,
    pub tls_disable_system_cas: Option<bool>,
    #[serde(default)]
    pub api_key_passthrough: bool,
}

#[derive(Debug, Deserialize)]
pub struct OpenAIModelConfig {
    #[serde(flatten)]
    pub base: BaseModelConfig,
    #[serde(default)]
    pub base_url: String,
    pub frequency_penalty: Option<f64>,
    pub max_tokens: Option<i32>,
    pub n: Option<i32>,
    pub presence_penalty: Option<f64>,
    pub reasoning_effort: Option<String>,
    pub seed: Option<i32>,
    pub temperature: Option<f64>,
    pub timeout: Option<i32>,
    pub top_p: Option<f64>,
}

#[derive(Debug, Deserialize)]
pub struct AzureOpenAIModelConfig {
    #[serde(flatten)]
    pub base: BaseModelConfig,
}

#[derive(Debug, Deserialize)]
pub struct AnthropicModelConfig {
    #[serde(flatten)]
    pub base: BaseModelConfig,
    #[serde(default)]
    pub base_url: String,
    pub max_tokens: Option<i32>,
    pub temperature: Option<f64>,
    pub top_p: Option<f64>,
    pub top_k: Option<i32>,
    pub timeout: Option<i32>,
}

#[derive(Debug, Deserialize)]
pub struct GeminiModelConfig {
    #[serde(flatten)]
    pub base: BaseModelConfig,
}

#[derive(Debug, Deserialize)]
pub struct GeminiVertexAIModelConfig {
    #[serde(flatten)]
    pub base: BaseModelConfig,
}

#[derive(Debug, Deserialize)]
pub struct GeminiAnthropicModelConfig {
    #[serde(flatten)]
    pub base: BaseModelConfig,
}

#[derive(Debug, Deserialize)]
pub struct OllamaModelConfig {
    #[serde(flatten)]
    pub base: BaseModelConfig,
    #[serde(default)]
    pub options: HashMap<String, String>,
}

#[derive(Debug, Deserialize)]
pub struct BedrockModelConfig {
    #[serde(flatten)]
    pub base: BaseModelConfig,
    #[serde(default)]
    pub region: String,
}

// ---------- Tool types ----------

#[derive(Debug, Deserialize)]
pub struct StreamableHTTPConnectionParams {
    pub url: String,
    #[serde(default)]
    pub headers: HashMap<String, String>,
    pub timeout: Option<f64>,
    pub sse_read_timeout: Option<f64>,
    pub terminate_on_close: Option<bool>,
    pub tls_insecure_skip_verify: Option<bool>,
    pub tls_ca_cert_path: Option<String>,
    pub tls_disable_system_cas: Option<bool>,
}

#[derive(Debug, Deserialize)]
pub struct HttpMcpServerConfig {
    pub params: StreamableHTTPConnectionParams,
    #[serde(default)]
    pub tools: Vec<String>,
    #[serde(default)]
    pub allowed_headers: Vec<String>,
    #[serde(default)]
    pub require_approval: Vec<String>,
}

#[derive(Debug, Deserialize)]
pub struct SseConnectionParams {
    pub url: String,
    #[serde(default)]
    pub headers: HashMap<String, String>,
    pub timeout: Option<f64>,
    pub sse_read_timeout: Option<f64>,
    pub tls_insecure_skip_verify: Option<bool>,
    pub tls_ca_cert_path: Option<String>,
    pub tls_disable_system_cas: Option<bool>,
}

#[derive(Debug, Deserialize)]
pub struct SseMcpServerConfig {
    pub params: SseConnectionParams,
    #[serde(default)]
    pub tools: Vec<String>,
    #[serde(default)]
    pub allowed_headers: Vec<String>,
    #[serde(default)]
    pub require_approval: Vec<String>,
}

// ---------- Remote agents ----------

#[derive(Debug, Deserialize)]
pub struct RemoteAgentConfig {
    pub name: String,
    pub url: String,
    #[serde(default)]
    pub headers: HashMap<String, String>,
    #[serde(default)]
    pub description: String,
}

// ---------- Memory ----------

#[derive(Debug, Deserialize)]
pub struct MemoryConfig {
    #[serde(default)]
    pub ttl_days: i32,
    pub embedding: Option<EmbeddingConfig>,
}

#[derive(Debug, Deserialize)]
pub struct EmbeddingConfig {
    pub provider: String,
    pub model: String,
    #[serde(default)]
    pub base_url: String,
}

// ---------- Context ----------

#[derive(Debug, Deserialize)]
pub struct AgentContextConfig {
    pub compaction: Option<AgentCompressionConfig>,
}

#[derive(Debug, Deserialize)]
pub struct AgentCompressionConfig {
    pub compaction_interval: Option<i32>,
    pub overlap_size: Option<i32>,
    pub summarizer_model: Option<Value>,
    #[serde(default)]
    pub prompt_template: String,
    pub token_threshold: Option<i32>,
    pub event_retention_size: Option<i32>,
}

// ---------- Agent card ----------

#[derive(Debug, Deserialize)]
pub struct AgentCard {
    pub name: String,
    #[serde(default)]
    pub description: String,
    #[serde(default)]
    pub version: String,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_deserialize_openai_config() {
        let json = r#"{
            "model": {
                "type": "openai",
                "model": "gpt-4o",
                "base_url": "https://api.openai.com/v1",
                "headers": {"Authorization": "Bearer sk-test"},
                "temperature": 0.7
            },
            "description": "Test agent",
            "instruction": "You are helpful",
            "stream": true
        }"#;
        let config: AgentConfig = serde_json::from_str(json).unwrap();
        assert!(matches!(config.model, ModelConfig::OpenAI(_)));
        assert_eq!(config.description, "Test agent");
        assert!(config.get_stream());
    }

    #[test]
    fn test_deserialize_anthropic_config() {
        let json = r#"{
            "model": {
                "type": "anthropic",
                "model": "claude-sonnet-4-5-20250929",
                "max_tokens": 4096
            },
            "description": "",
            "instruction": "Be helpful"
        }"#;
        let config: AgentConfig = serde_json::from_str(json).unwrap();
        assert!(matches!(config.model, ModelConfig::Anthropic(_)));
        assert!(!config.get_stream());
    }

    #[test]
    fn test_deserialize_all_model_types() {
        let types = [
            ("openai", r#"{"type":"openai","model":"gpt-4o","base_url":""}"#),
            ("azure_openai", r#"{"type":"azure_openai","model":"gpt-4o"}"#),
            ("anthropic", r#"{"type":"anthropic","model":"claude-3","base_url":""}"#),
            ("gemini", r#"{"type":"gemini","model":"gemini-pro"}"#),
            ("gemini_vertex_ai", r#"{"type":"gemini_vertex_ai","model":"gemini-pro"}"#),
            ("gemini_anthropic", r#"{"type":"gemini_anthropic","model":"claude-3"}"#),
            ("ollama", r#"{"type":"ollama","model":"llama2"}"#),
            ("bedrock", r#"{"type":"bedrock","model":"anthropic.claude-v2","region":"us-east-1"}"#),
        ];
        for (name, model_json) in types {
            let json = format!(
                r#"{{"model": {model_json}, "description": "", "instruction": ""}}"#,
            );
            let result = serde_json::from_str::<AgentConfig>(&json);
            assert!(result.is_ok(), "Failed to deserialize model type: {name}");
        }
    }

    #[test]
    fn test_deserialize_with_tools() {
        let json = r#"{
            "model": {"type": "openai", "model": "gpt-4o", "base_url": ""},
            "description": "",
            "instruction": "",
            "http_tools": [{
                "params": {"url": "http://localhost:8080/mcp", "headers": {}},
                "tools": ["read_file", "write_file"],
                "require_approval": ["write_file"]
            }],
            "sse_tools": [{
                "params": {"url": "http://localhost:8081/sse", "headers": {}},
                "tools": []
            }]
        }"#;
        let config: AgentConfig = serde_json::from_str(json).unwrap();
        assert_eq!(config.http_tools.len(), 1);
        assert_eq!(config.http_tools[0].tools, vec!["read_file", "write_file"]);
        assert_eq!(config.http_tools[0].require_approval, vec!["write_file"]);
        assert_eq!(config.sse_tools.len(), 1);
    }
}
