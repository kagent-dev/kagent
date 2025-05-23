#!/bin/bash

SCRIPT_DIR=$(cd $(dirname ${BASH_SOURCE[0]}); pwd)

# Make sure envsubst is available
if ! command -v envsubst &> /dev/null; then
  echo "Installing gettext package for envsubst..."
  
  # Detect the operating system for installing the right package
  if [ "$(uname)" == "Darwin" ]; then
    # macOS
    brew install gettext
    brew link --force gettext
  elif [ -f /etc/debian_version ]; then
    # Debian/Ubuntu
    sudo apt-get update
    sudo apt-get install -y gettext
  elif [ -f /etc/redhat-release ]; then
    # RHEL/CentOS/Fedora
    sudo yum install -y gettext
  else
    echo "Unsupported OS. Please install gettext package manually."
    exit 1
  fi
fi

# Check if required environment variables are set
if [ -z "${OPENAI_API_KEY}" ] || [ -z "${QDRANT_API_KEY}" ]; then
  echo "Error: Required environment variables are not set. Please set them before running this script."
  echo "Example:"
  echo "export OPENAI_API_KEY=\"your-openai-api-key\""
  echo "export QDRANT_API_KEY=\"your-qdrant-api-key\""
  exit 1
fi

make build-all
make create-kind-cluster

make build-cli-local
sudo mv go/bin/kagent-local /usr/local/bin/kagent
make kind-load-docker-images
make helm-install

kubectl apply -f "${SCRIPT_DIR}/resources/agent.yaml"
kubectl apply -f "${SCRIPT_DIR}/resources/tool-check.yaml"
kubectl apply -f "${SCRIPT_DIR}/resources/model.yaml"

# Use environment variable substitution to create the final YAML and apply it
envsubst < "${SCRIPT_DIR}/resources/tool-docs.template.yaml" | kubectl apply -f -
