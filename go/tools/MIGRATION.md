

# Migration Guide

## 1. Migrating from Python tools to Go tools

Python tools located at `python/src/kagent/tools`

Before migrating ensure python project is working correctly by running build and tests:
```bash
make -C python build
make -C python test
```

List of tools in `python/src/kagent/tools`:
```bash
cd python && uv run kagent-engine --help
```
Run each tool to see its functionality:

```bash
cd python &&  uv run kagent-engine k8s                                                                                                                                                                                                                                                                                                                                   │
cd python &&  uv run kagent-engine prometheus                                                                                                                                                                                                                                                                                                                            │
cd python &&  uv run kagent-engine argo                                                                                                                                                                                                                                                                                                                                  │
cd python &&  uv run kagent-engine istio                                                                                                                                                                                                                                                                                                                                 │
cd python &&  uv run kagent-engine helm 
```

### Migration Steps
1. Identify the Python tool you want to migrate.
2. Write Go test for python tool server
