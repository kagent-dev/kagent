# Oneshot Test Configurations

Sample `config.json` files for testing each model provider with the oneshot tool.

## Usage

```bash
cd go/adk
go run ./examples/oneshot -config examples/oneshot/testdata/<provider>.json -task "What is 2+2?"
```

## Configs and Required Environment Variables

| Config | Provider | Required Env Vars |
|--------|----------|-------------------|
| `openai.json` | OpenAI | `OPENAI_API_KEY` |
| `openai_custom.json` | OpenAI (temp=0, max_tokens=100) | `OPENAI_API_KEY` |
| `anthropic.json` | Anthropic | `ANTHROPIC_API_KEY` |
| `gemini.json` | Gemini (API key) | `GOOGLE_API_KEY` or `GEMINI_API_KEY` |
| `gemini_vertexai.json` | Gemini on Vertex AI | `GOOGLE_CLOUD_PROJECT`, `GOOGLE_CLOUD_LOCATION`, ADC |
| `anthropic_vertexai.json` | Anthropic on Vertex AI | `GOOGLE_CLOUD_PROJECT`, `GOOGLE_CLOUD_LOCATION`, ADC |
| `ollama.json` | Ollama (local) | Ollama running at `localhost:11434` (or `OLLAMA_API_BASE`) |
| `azure_openai.json` | Azure OpenAI | `AZURE_OPENAI_API_KEY`, `AZURE_OPENAI_ENDPOINT` |
| `bedrock.json` | AWS Bedrock | `AWS_REGION`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` (or IAM role) |
