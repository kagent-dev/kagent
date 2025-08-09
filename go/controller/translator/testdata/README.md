# API Translator Golden Tests

This directory contains golden tests for the API translator. Golden tests are a type of test where the expected output is stored in files and compared against the actual output.

## Structure

```
testdata/
├── inputs/     # Input YAML files containing test scenarios
├── outputs/    # Expected output JSON files (golden files)
└── README.md   # This file
```

## Input File Format

Each input file in `inputs/` is a YAML file with the following structure:

```yaml
operation: translateAgent  # The operation to test: "translateAgent", "translateTeam", or "translateToolServer"
targetObject: agent-name   # The name of the object to translate
namespace: test           # The namespace where objects are located
objects:                  # List of Kubernetes objects needed for the test
  - apiVersion: v1
    kind: Secret
    metadata:
      name: api-secret
      namespace: test
    data:
      api-key: base64-encoded-key
  - apiVersion: kagent.dev/v1alpha1
    kind: ModelConfig
    # ... more objects
```

## Test Cases

### Current Test Cases

1. **basic_agent.yaml** - A basic agent with OpenAI model and no tools
2. **agent_with_builtin_tools.yaml** - Agent with builtin tools (Prometheus and Docs tools)
3. **agent_with_memory.yaml** - Agent with Pinecone vector memory
4. **anthropic_agent.yaml** - Agent using Anthropic Claude model
5. **ollama_agent.yaml** - Agent using Ollama local model
6. **agent_with_nested_agent.yaml** - Agent with nested agent tools

### Adding New Test Cases

To add a new test case:

1. Create a new YAML file in `inputs/` following the format above
2. Include all necessary Kubernetes objects (Secrets, ModelConfigs, Agents, etc.)
3. Run the test with `UPDATE_GOLDEN=true` to generate the expected output
4. Verify the generated output is correct
5. Commit both the input and output files

## Running Tests

### Run all golden tests:
```bash
go test -run TestGoldenTranslator ./go/controller/translator/
```

### Update golden files (regenerate expected outputs):
```bash
UPDATE_GOLDEN=true go test -run TestGoldenTranslator ./go/controller/translator/
```

### Run specific test:
```bash
go test -run TestGoldenTranslator/basic_agent ./go/controller/translator/
```

## Test Coverage

The golden tests cover various scenarios:

- **Model Providers**: OpenAI, Anthropic, Ollama
- **Tools**: Builtin tools (with model client injection and API key injection), nested agent tools
- **Memory**: Pinecone vector memory
- **Configuration**: Various model parameters, environment variables, secrets

## Notes

- Golden files are automatically normalized to remove non-deterministic fields like IDs and timestamps
- Tests use fake Kubernetes clients, so no actual cluster is needed
- All sensitive data in test files uses dummy values
- The tests focus on `TranslateGroupChatForAgent` functionality 