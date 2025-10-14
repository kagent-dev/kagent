#!/bin/bash
# E2E Test Runner for KAgent Shared Sessions
#
# This script runs E2E tests with automatic API URL detection from MetalLB.
# It also verifies prerequisites before running tests.
#
# Usage:
#   ./run_tests.sh                    # Run with rebuild (Constitution requirement)
#   ./run_tests.sh --skip-rebuild     # Skip rebuild (for faster iteration)
#   ./run_tests.sh [pytest args]      # Pass args to pytest

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../../../.." && pwd)"

# Parse flags
SKIP_REBUILD=false
PYTEST_ARGS=()

for arg in "$@"; do
    if [ "$arg" = "--skip-rebuild" ]; then
        SKIP_REBUILD=true
    else
        PYTEST_ARGS+=("$arg")
    fi
done

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "======================================"
echo "KAgent E2E Test Runner"
echo "======================================"
echo ""

# Check prerequisites
echo "Checking prerequisites..."

# Check kubectl
if ! command -v kubectl &> /dev/null; then
    echo -e "${RED}✗ kubectl not found${NC}"
    echo "Install kubectl: https://kubernetes.io/docs/tasks/tools/"
    exit 1
fi
echo -e "${GREEN}✓ kubectl found${NC}"

# Check cluster connectivity
if ! kubectl cluster-info &> /dev/null; then
    echo -e "${RED}✗ Cannot connect to Kubernetes cluster${NC}"
    echo "Start your cluster with: make create-kind-cluster"
    exit 1
fi
echo -e "${GREEN}✓ Kubernetes cluster accessible${NC}"

# Check kagent namespace
if ! kubectl get namespace kagent &> /dev/null; then
    echo -e "${RED}✗ kagent namespace not found${NC}"
    echo "Deploy KAgent with: make helm-install"
    exit 1
fi
echo -e "${GREEN}✓ kagent namespace exists${NC}"

# Check kagent-controller service
if ! kubectl get svc kagent-controller -n kagent &> /dev/null; then
    echo -e "${RED}✗ kagent-controller service not found${NC}"
    echo "Deploy KAgent with: make helm-install"
    exit 1
fi
echo -e "${GREEN}✓ kagent-controller service exists${NC}"

# Check default-model-config
if ! kubectl get modelconfig default-model-config -n kagent &> /dev/null; then
    echo -e "${YELLOW}⚠ default-model-config not found${NC}"
    echo "This may cause test failures. Deploy with: make helm-install"
fi

# Detect API URL
echo ""
echo "Detecting KAGENT_API_URL..."
LB_IP=$(kubectl get svc kagent-controller -n kagent -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || echo "")
LB_HOSTNAME=$(kubectl get svc kagent-controller -n kagent -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null || echo "")
PORT=$(kubectl get svc kagent-controller -n kagent -o jsonpath='{.spec.ports[0].port}' 2>/dev/null || echo "8083")

if [ -n "$KAGENT_API_URL" ]; then
    echo -e "${GREEN}Using KAGENT_API_URL from environment: $KAGENT_API_URL${NC}"
elif [ -n "$LB_IP" ]; then
    export KAGENT_API_URL="http://${LB_IP}:${PORT}"
    echo -e "${GREEN}Detected LoadBalancer IP: $KAGENT_API_URL${NC}"
elif [ -n "$LB_HOSTNAME" ]; then
    export KAGENT_API_URL="http://${LB_HOSTNAME}:${PORT}"
    echo -e "${GREEN}Detected LoadBalancer hostname: $KAGENT_API_URL${NC}"
else
    export KAGENT_API_URL="http://localhost:8083"
    echo -e "${YELLOW}Using fallback: $KAGENT_API_URL${NC}"
    echo -e "${YELLOW}Note: You may need to run: kubectl port-forward -n kagent svc/kagent-controller 8083:8083${NC}"
fi

# E2E Test Preparation (per Constitution 1.1.0)
if [ "$SKIP_REBUILD" = false ]; then
    echo ""
    echo "======================================"
    echo "E2E Test Preparation"
    echo "======================================"
    echo ""
    echo "Per KAgent Constitution 1.1.0: E2E tests require fresh deployment"
    echo ""

    # Rebuild project
    echo "Rebuilding project..."
    cd "${REPO_ROOT}"
    if make build; then
        echo -e "${GREEN}✓ Build successful${NC}"
    else
        echo -e "${RED}✗ Build failed${NC}"
        exit 1
    fi

    # Redeploy with Helm
    echo ""
    echo "Redeploying KAgent..."
    if make helm-install; then
        echo -e "${GREEN}✓ Helm install successful${NC}"
    else
        echo -e "${RED}✗ Helm install failed${NC}"
        exit 1
    fi

    # Restart all pods
    echo ""
    echo "Restarting all pods in kagent namespace..."
    if kubectl delete po --all -n kagent &> /dev/null; then
        echo -e "${GREEN}✓ Pods deleted${NC}"
    else
        echo -e "${YELLOW}⚠ Pod deletion warning (may be ok)${NC}"
    fi

    # Wait for pods to be ready
    echo ""
    echo "Waiting for pods to be ready..."
    sleep 5
    if kubectl wait --for=condition=ready pod --all -n kagent --timeout=120s &> /dev/null; then
        echo -e "${GREEN}✓ All pods ready${NC}"
    else
        echo -e "${YELLOW}⚠ Some pods may not be ready. Continuing anyway...${NC}"
    fi
else
    echo ""
    echo -e "${YELLOW}⚠ Skipping rebuild (--skip-rebuild flag)${NC}"
    echo -e "${YELLOW}  Note: This violates Constitution 1.1.0 E2E Test Preparation${NC}"
    echo -e "${YELLOW}  Use only for development iteration. Production tests MUST rebuild.${NC}"
    echo ""
fi

# Test connectivity and setup port-forward if needed
echo ""
echo "Testing API connectivity..."
sleep 2  # Give API a moment to fully start

PORT_FORWARD_PID=""
echo "Setting up kubectl port-forward..."

# Kill any existing port-forward on port 8083
pkill -9 -f "kubectl port-forward" 2>&1 > /dev/null || true

# Start port-forward in background
kubectl port-forward -n kagent service/kagent-controller 8083:8083 &> /tmp/kubectl-port-forward.log &
PORT_FORWARD_PID=$!
sleep 3

# Update API URL to use port-forward
export KAGENT_API_URL="http://localhost:8083"

# Test again
if curl -s -f "${KAGENT_API_URL}/health" &> /dev/null; then
    echo -e "${GREEN}✓ API reachable via port-forward at ${KAGENT_API_URL}${NC}"
else
    echo -e "${RED}✗ Cannot reach API even with port-forward${NC}"
    echo "Check logs: tail /tmp/kubectl-port-forward.log"
    exit 1
fi


# Run tests
echo ""
echo "======================================"
echo "Running E2E Tests"
echo "======================================"
echo ""

cd "${REPO_ROOT}"

# Prepare pytest arguments
if [ ${#PYTEST_ARGS[@]} -eq 0 ]; then
    PYTEST_ARGS=("python/packages/kagent-adk/tests/e2e/test_shared_sessions.py" "-v")
fi

# Run pytest
uv run pytest "${PYTEST_ARGS[@]}"

# Capture exit code
EXIT_CODE=$?

# Cleanup port-forward if we started it
if [ -n "$PORT_FORWARD_PID" ]; then
    echo ""
    echo "Cleaning up port-forward (PID: $PORT_FORWARD_PID)..."
    kill $PORT_FORWARD_PID 2>/dev/null || true
fi

echo ""
echo "======================================"
if [ $EXIT_CODE -eq 0 ]; then
    echo -e "${GREEN}✓ E2E Tests PASSED${NC}"
else
    echo -e "${RED}✗ E2E Tests FAILED${NC}"
fi
echo "======================================"

exit $EXIT_CODE

