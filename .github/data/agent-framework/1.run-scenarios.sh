#!/bin/bash
set -euo pipefail

# Define a function to log messages with timestamp
log() {
  echo "[$(date +'%Y-%m-%dT%H:%M:%S')] $1"
}

export CLUSTER_CTX=kind-kagent
# Loop through each challenge defined in the .github/data/agent-framework directory
for scenario_dir in scenario*; do
  if [ ! -d "$scenario_dir" ]; then
    continue
  fi

  npm i || pnpm i
  echo "pwd=$(pwd)"
  for challenge_path in ${scenario_dir}/*.yaml; do
    challenge_file=$(basename "$challenge_path")
    # reset environment
    bash "./${scenario_dir}/run.sh"
    bash ./run-challenge.sh "$scenario_dir" "$challenge_file"
    kubectl --context "${CLUSTER_CTX}" delete deploy --all -n default
  done
done
