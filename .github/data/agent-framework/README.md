# Kubernetes Agent Benchmark


1. From the root of the repository, run the command below. You can make it faster by setting your architecture to `amd64` or `arm64`:

```bash
export BUILD_ARGS="--platform linux/amd64"
bash .github/data/agent-framework/0.setup.sh
```

Validate that the `kagent` cli is setup and the cluster is running:

```bash
kagent version
kubectl get pods -A
```

2. **Run individual challenges** by navigating to the `.github/data/agent-framework` running the following command:

```bash
export CLUSTER_CTX=kind-kagent
cd .github/data/agent-framework
scenario1/run.sh
npm i
npm i -g mocha

# ../run-challenge.sh scenario1 <challenge-name>
./run-challenge.sh scenario1 deployment-probe-failures.yaml
```

or 

2. Run all challenges at once:

```bash
./1.run-scenarios.sh
```
