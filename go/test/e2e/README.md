# debugging

How to debug an agent locally without kagent (so there's less noise). This allows reproducing the 
same scenario as in the e2e test with just the agent and a mock LLM server.

## Get agent config

### Option 1
Copy over agent config from the cluster

```bash
TEMP_DIR=$(mktemp -d)
kubectl exec -n kagent -ti deploy/test-agent -c kagent -- tar c -C / config | tar -x -C ${TEMP_DIR}
AGENT_CONFIG_DIR=${TEMP_DIR}/config
```

Edit config to point to local mock server

```bash
jq '.model.base_url="http://127.0.0.1:8090/v1"' ${AGENT_CONFIG_DIR}/config/config.json > ${AGENT_CONFIG_DIR}/config/config_tmp.json && mv ${AGENT_CONFIG_DIR}/config/config_tmp.json ${AGENT_CONFIG_DIR}/config/config.json
```

### Option 2

Generate agent generic config. If using `@modelcontextprotocol/server-everything` start it first (`npx -y @modelcontextprotocol/server-everything streamableHttp`), so it is added to the tools.

```bash
(cd go; go run hack/makeagentconfig/main.go)
AGENT_CONFIG_DIR=$PWD/go
```

tweak it as needed to match the e2e test (e.g., add tools, set the model, etc)

## Start mock LLM server
Start local mock LLM server (use json from e2e test):

```bash
(cd go; go run hack/mockllm/main.go invoke_mcp_agent.json) &
```


Now this should work (in task, use prompt from e2e test)!

```bash
export OPENAI_API_KEY=dummykey
cd python
uv run kagent-adk test --filepath ${AGENT_CONFIG_DIR} --task "Tell me a joke"
```


## With skills

```bash
export KAGENT_SKILLS_FOLDER=$PWD/go/test/e2e/testdata/skills/
export OPENAI_API_KEY=dummykey
cd python
uv run kagent-adk test --filepath ${AGENT_CONFIG_DIR} --task "Tell me a joke"
```

