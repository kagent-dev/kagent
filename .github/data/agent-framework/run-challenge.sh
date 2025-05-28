#!/bin/bash

scenario_dir="$1"
challenge_file="$2"

# Extract the challenge name and description from the YAML metadata file
NAME=$(yq eval '.metadata.name' "${scenario_dir}/${challenge_file}")
DESCRIPTION=$(yq eval '.spec.description' "${scenario_dir}/${challenge_file}")
USER_PROMPT=$(yq eval '.spec.prompt' "${scenario_dir}/${challenge_file}")

log() {
  echo "[$(date +'%Y-%m-%dT%H:%M:%S')] $1"
}

# Run the challenge scenario using a Bash script generated from markdown in the README file
log "*********************************************************************"
log "Running challenge: $NAME - $DESCRIPTION"
log "*********************************************************************"
log "User Prompt: $USER_PROMPT"


echo "Waiting for pods to be stable..."
while kubectl --context "${CLUSTER_CTX}" get pods -A | grep ContainerCreating; do sleep 5; done
while kubectl --context "${CLUSTER_CTX}" get pods -A | grep Terminating; do sleep 5; done

# Test baseline
log "Testing initial cluster state..."
timeout --signal=INT 3m mocha "${scenario_dir}/test.js" --timeout 10000 --retries 5
BASELINE_TEST_STATUS=$?

if [ $BASELINE_TEST_STATUS -ne 0 ]; then
    log "ERROR: Baseline test failed. The cluster is not in the right state to proceed with the challenge."
    log "Exiting without breaking the environment."
    exit 1
fi

# Break the environment by executing commands defined in each step of the challenge
log "Breaking the environment..."
STEPS_COUNT=$(yq '.spec.steps | length' "${scenario_dir}/${challenge_file}")
for ((i=0; i<$STEPS_COUNT; i++)); do
    yq ".spec.steps[$i].run" "${scenario_dir}/${challenge_file}" | while IFS= read -r cmd; do
    echo "$cmd" >> "${scenario_dir}/${challenge_file}".$i.sh
    done
    echo "Waiting for pods to be stable..."
while kubectl --context ${CLUSTER_CTX} get pods -A | grep ContainerCreating; do sleep 5; done
while kubectl --context ${CLUSTER_CTX} get pods -A | grep Terminating; do sleep 5; done

    sh "${scenario_dir}/${challenge_file}".$i.sh
done
rm -f "$challenge_file".*.sh
echo "Waiting for pods to be stable..."
# while kubectl --context ${CLUSTER_CTX} get pods -A | grep ContainerCreating; do sleep 5; done
while kubectl --context ${CLUSTER_CTX} get pods -A | grep Terminating; do sleep 5; done
kubectl --context ${CLUSTER_CTX} get pods -A

log "Testing cluster after breaking..."
timeout --signal=INT 1m mocha "${scenario_dir}/test.js" --timeout 10000 || true

# Try to fix the broken environment using Kagent
log "Trying to fix the kagent broken environment using Kagent..."

# Pipe the output of kagent invoke to the thought log file
touch "${scenario_dir}/$NAME.thought.log"
mkdir -p "${scenario_dir}/results"

timeout --signal=INT 3m bash -c 'echo "$1" | kagent invoke -v --agent "k8s-agent" -S --task -' -- "$USER_PROMPT" > "${scenario_dir}/results/$NAME.thought.log" 2>&1

TIMEOUT_STATUS=$?
if [ $TIMEOUT_STATUS -eq 124 ]; then
  log "Kagent invoke command timed out. Exiting immediately."
  echo "TIMED OUT" >> "${scenario_dir}/results/$NAME.failure"
  exit 1
fi

log "Testing cluster after fixing..."
kubectl --context ${CLUSTER_CTX} get pods -A
if mocha "${scenario_dir}/test.js" --timeout 10000; then
  log "---------------> challenge SUCCESSFUL <------------------"
  rm -f "${scenario_dir}/$NAME.failure" || true
  mv "${scenario_dir}/$NAME.thought.log" "${scenario_dir}/results/$NAME.success"
else
  log "---------------> challenge FAILED <----------------------"
  rm -f "${scenario_dir}/$NAME.success" || true
  mv "${scenario_dir}/$NAME.thought.log" "${scenario_dir}/results/$NAME.failure"
fi

