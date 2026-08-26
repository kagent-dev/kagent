# Trying the UI against a real cluster

One command builds a Kind cluster and installs **this checkout** on it — the controller
and the UI are both built from the working tree and swapped in over the chart's
published images, so what runs on the cluster is the code you are reviewing.

```sh
./scripts/setup-cluster/setup-cluster.sh          # ~20 min, mostly image builds
```

Then forward the UI and open it:

```sh
kubectl -n kagent port-forward svc/kagent-ui 8080:8080
```

**http://localhost:8080**

The script leaves one agent on the cluster — an `assistant` template on a `kagent`
harness — so **Agents** has something in it and you can start a conversation straight
away. Add more from **Agents → New harness / New template**; a template is only run by
a harness that admits its labels, and with a single harness on the cluster the
new-template form applies those labels for you.

## Iterating on the UI

The dev server talks to the same cluster, with hot reload, and is the better loop while
changing code. Nothing to configure — the script forwards the controller on `8083`,
which is where `yarn dev` looks by default:

```sh
cd ui
yarn dev
```

**http://localhost:8001**

That is the whole reason this script holds two forwards open rather than one. It used to
forward only the UI, and running the dev server then needed
`KAGENT_DEV_CONTROLLER_URL=http://127.0.0.1:8080` in `ui/.env` — a line whose absence
looked like a broken backend rather than a missing forward, since the page loaded fine
and every read failed with `ECONNREFUSED 127.0.0.1:8083`.

Setting that line is still worth doing when what you want to exercise is the proxy hop
itself: at `8080` the dev server goes through the UI pod's nginx exactly as a deployed
build does, rather than straight to the controller.

Worth knowing which one you are looking at: the dev server does not exercise the
Dockerfile, nginx, or `scripts/init.sh` rendering settings at start. To see a change
the way it will ship, rebuild the image and swap it in:

```sh
docker buildx build --push --platform linux/arm64 \
  -t localhost:5001/kagent-dev/kagent/ui:dev -f ui/Dockerfile ./ui
kubectl -n kagent rollout restart deploy/kagent-ui
```

## No cluster at all

If you would rather not build one, the UI runs entirely on in-browser fixtures:

```sh
cd ui
ENABLE_MOCK_UI=true yarn dev
```

Every page works and says on the page that the data is not real. `?mock=empty`,
`?mock=error` and `?mock=slow` pick which scenario the fixtures play.

## Before the first run

`docker`, `kind`, `kubectl`, `helm`, `jq`, `openssl`, `yarn`. Set `OPENAI_API_KEY` if you
want agents that answer; without it everything installs and chats fail at the model call.

## Starting over

```sh
kind delete cluster --name kagent
docker rm -f $(docker ps -aq)
docker volume prune -f
```

## Why this is a script and not four commands

Five things make the obvious path fail, and each fails silently:

- **`make create-kind-cluster && make helm-install` does not work.** Every substrate
  workload mounts secrets that do not exist until the `kubectl-ate` pool commands have
  run, so the controller crash-loops while its pod reports `1/1 Ready`.
- **The chart deploys `controller-v2`**, which does not implement agents, models, tool
  servers or prompt libraries — those pages read an empty API. The script builds
  `core/cmd/controller/main.go` and swaps it in. Note that `make build-controller`
  builds v2, the other one.
- **`kubectl-ate` exits 0 slightly before its secret is readable**, so the next step
  waits for the secret rather than trusting the exit code.
- **The chart installs published images**, so a cluster built without this script runs
  somebody else's build of both the controller and the UI, and none of the local
  changes are on it — while everything looks installed and healthy.
- **A harness runs the Go ADK, not the Python one.** An actor starts by restoring its
  template's golden snapshot, and the Python runtime does not survive that — it comes
  back with `Fatal Python error: Illegal instruction` and never serves `/readyz`, so
  the harness sits in `ResumeGoldenActor` and every message times out at the router
  with a 504. A static Go binary restores cleanly. The chart names `golang-adk` as the
  image for declarative agents, and the script builds that.

## When a page looks wrong

One command distinguishes a broken transport from an empty list, which look identical:

```sh
printf '\x00\x00\x00\x00\x00' > /tmp/gw.bin
curl -s -D - -X POST -H 'Content-Type: application/grpc-web+proto' -H 'X-Grpc-Web: 1' \
  --data-binary @/tmp/gw.bin \
  http://127.0.0.1:8083/api/kagent.api.v1alpha1.SystemService/GetVersion | head
```

`grpc-status: 0` with a framed body means the whole path is up. An empty `200` means no
gRPC-Web handler is mounted; `status 12` means that service is not registered in the
binary you are running.

Restarting the controller kills the port-forward and the UI reports it as a failed read,
so start it again before concluding anything is broken.
