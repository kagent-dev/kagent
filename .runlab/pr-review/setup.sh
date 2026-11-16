#!/bin/bash
# pr-review: the tools kagent's development setup needs, a clone of main, a kind cluster with its registry and
# MetalLB made by the project's own scripts, and the build caches warmed by one `make build`. The cluster is left
# stopped; story/ready.sh starts it at boot.
# Kagent and Substrate are NOT installed here: Substrate's pod certificates last 24 hours and are not renewed after a
# resume, so the lesson installs them for each pull request.
# Runs as root after the common setup. Docker is already enabled.
set -euo pipefail
trap '' HUP

export DEBIAN_FRONTEND=noninteractive
export HOME=/root

GO_VERSION=1.27.3
KIND_VERSION=v0.33.0
HELM_VERSION=v3.19.0

echo "pr-review setup starting"

apt-get update
apt-get install -y jq git make build-essential curl ca-certificates

fetch() { curl -fL --retry 3 -o "$1" "$2"; }

# kubectl
kubectl_version=$(curl -fsSL https://dl.k8s.io/release/stable.txt)
fetch /usr/local/bin/kubectl "https://dl.k8s.io/release/${kubectl_version}/bin/linux/amd64/kubectl"
chmod 755 /usr/local/bin/kubectl
kubectl completion bash > /usr/share/bash-completion/completions/kubectl

# kind
fetch /tmp/kind "https://github.com/kubernetes-sigs/kind/releases/download/${KIND_VERSION}/kind-linux-amd64"
install -m 755 /tmp/kind /usr/local/bin/kind

# helm
fetch /tmp/helm.tgz "https://get.helm.sh/helm-${HELM_VERSION}-linux-amd64.tar.gz"
tar -C /tmp -xzf /tmp/helm.tgz linux-amd64/helm
install -m 755 /tmp/linux-amd64/helm /usr/local/bin/helm

# Go: the local CLI build (kagent-cli-install) uses it; the images build inside docker.
fetch /tmp/go.tgz "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"
rm -rf /usr/local/go
tar -C /usr/local -xzf /tmp/go.tgz
ln -sf /usr/local/go/bin/go /usr/local/bin/go
rm -rf /tmp/kind /tmp/helm* /tmp/linux-amd64 /tmp/go.tgz

# docker buildx (the project asks for 0.23 or later)
if ! docker buildx version >/dev/null 2>&1; then
  apt-get install -y docker-buildx-plugin || {
    mkdir -p /usr/local/lib/docker/cli-plugins
    fetch /usr/local/lib/docker/cli-plugins/docker-buildx \
      https://github.com/docker/buildx/releases/download/v0.25.0/buildx-v0.25.0.linux-amd64
    chmod 755 /usr/local/lib/docker/cli-plugins/docker-buildx
  }
fi

# kind and containerd watch a lot of files. The image defaults are too small.
cat > /etc/sysctl.d/99-inotify.conf << 'EOF2'
fs.inotify.max_user_watches = 524288
fs.inotify.max_user_instances = 512
EOF2
sysctl --system >/dev/null

cp -r /root/data/. /root/ 2>/dev/null || true
chown root:root /root
chmod 700 /root

cat >> /root/.bashrc << 'EOF2'

alias k=kubectl
if [ -f /usr/share/bash-completion/completions/kubectl ]; then
  . /usr/share/bash-completion/completions/kubectl
  complete -o default -F __start_kubectl k
fi
EOF2

# main, as of this build, with its full history. trylab gives the guest the pull request's commits as a git bundle of what
# this clone lacks (runlab#1227), and a bundle needs the commits it was cut from: a shallow clone would refuse it.
git clone https://github.com/kagent-dev/kagent /root/kagent
cd /root/kagent

# The kagent UI is reached through a kind port mapping: guest port 80 to node port 30080, where the lesson puts
# the UI service. Kind has no servicelb as k3d does, so without this the service button has nothing to reach.
sed -i 's/^  - role: control-plane$/  - role: control-plane\n    extraPortMappings:\n      - containerPort: 30080\n        hostPort: 80\n        listenAddress: "127.0.0.1"/' scripts/kind/kind-config.yaml
grep -q extraPortMappings scripts/kind/kind-config.yaml || { echo "the port mapping was not added to kind-config.yaml" >&2; exit 1; }

# The project's own scripts make the cluster, its registry and MetalLB.
make create-kind-cluster
make use-kind-cluster

# One build of main warms docker's layer cache, the Go modules and the yarn cache, and fills the registry.
# The provider key is a placeholder: the image must not hold a real key.
MODEL_PROVIDER=openAI OPENAI_API_KEY=sk-placeholder make build

# Warm the install: kagent, Substrate, PostgreSQL and the PodCertificate controller bring images that main's build does
# not. Install once so they are in the node's containerd, then uninstall. Nothing of the install may stay in the
# snapshot: Substrate's pod certificates last 24 hours and are not renewed after a resume. The lesson installs again.
MODEL_PROVIDER=openAI OPENAI_API_KEY=sk-placeholder make kagent-cli-install
./go/core/bin/kagent-local --config go/core/bin/local-config.yaml uninstall --delete-worker-pools
kubectl wait --for=delete namespace/ate-system namespace/podcertificate-controller-system --timeout=300s || true

# The UI service of the lesson's install is named kagent-ui and does not exist yet. This service finds the UI's pods by
# their labels whenever they appear, and the kind port mapping above leads node port 30080 to this machine's port 80.
kubectl apply -f - << 'EOF2'
apiVersion: v1
kind: Service
metadata:
  name: kagent-ui-public
  namespace: kagent
spec:
  type: NodePort
  selector:
    app.kubernetes.io/name: kagent
    app.kubernetes.io/component: ui
  ports:
  - name: ui
    port: 8080
    targetPort: 8080
    nodePort: 30080
EOF2

# The cluster and the registry are stopped in the image: story/ready.sh starts them when the lab boots, before the snapshot is taken.
docker stop kagent-control-plane kind-registry
echo "pr-review setup done"
