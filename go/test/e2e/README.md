# debugging

How to debug an agent locally without kagent (so there's less noise):


Copy over agent config from the cluster

```bash
TMP_DIR=$(mktemp -d)
kubectl exec -n kagent -ti deploy/test-agent -c kagent -- tar c -C / config | tar -x -C $TMP_DIR
```
Start local mock LLM server

```bash
(cd go; go run hack/mockllm.go invoke_mcp_agent.json) &
```

Edit config to point to local mock server

```bash
jq '.model.base_url="http://127.0.0.1:8090/v1"' $TMP_DIR/config/config.json > $TMP_DIR/config/config_tmp.json && mv $TMP_DIR/config/config_tmp.json $TMP_DIR/config/config.json
```

Now this should work!

```bash
export OPENAI_API_KEY=dummykey
cd python
uv run kagent-adk test --filepath $TMP_DIR/config --task "Tell me a joke"
```


## With skills

```bash
export KAGENT_SKILLS_FOLDER=$PWD/go/test/e2e/testdata/skills/
export OPENAI_API_KEY=dummykey
cd python
uv run kagent-adk test --filepath $TMP_DIR/config --task "Tell me a joke"
```

