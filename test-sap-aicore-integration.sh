#!/bin/bash

# SAP AI Core Integration Test Script for KAgent
# This script tests the SAP AI Core integration end-to-end

set -e

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
NAMESPACE="${NAMESPACE:-kagent}"
SECRET_NAME="kagent-sap-aicore"
MODELCONFIG_NAME="sap-aicore-test"
AGENT_NAME="sap-aicore-test-agent"

# Check required environment variables
required_vars=("SAP_AI_CORE_API_KEY" "SAP_AI_CORE_BASE_URL" "SAP_AI_CORE_DEPLOYMENT_ID")
for var in "${required_vars[@]}"; do
    if [ -z "${!var}" ]; then
        echo -e "${RED}Error: $var environment variable is not set${NC}"
        echo "Please set the following environment variables:"
        echo "  export SAP_AI_CORE_API_KEY='your-api-key'"
        echo "  export SAP_AI_CORE_BASE_URL='https://api.ai.prod.eu-central-1.aws.ml.hana.ondemand.com'"
        echo "  export SAP_AI_CORE_DEPLOYMENT_ID='your-deployment-id'"
        echo "  export SAP_AI_CORE_RESOURCE_GROUP='default'  # Optional, defaults to 'default'"
        exit 1
    fi
done

SAP_AI_CORE_RESOURCE_GROUP="${SAP_AI_CORE_RESOURCE_GROUP:-default}"

echo -e "${GREEN}=== SAP AI Core Integration Test ===${NC}"
echo ""

# Step 1: Create Secret
echo -e "${YELLOW}Step 1: Creating Kubernetes Secret...${NC}"
kubectl create secret generic $SECRET_NAME \
    --from-literal=SAP_AI_CORE_API_KEY="$SAP_AI_CORE_API_KEY" \
    --namespace=$NAMESPACE \
    --dry-run=client -o yaml | kubectl apply -f -
echo -e "${GREEN}✓ Secret created${NC}"
echo ""

# Step 2: Create ModelConfig
echo -e "${YELLOW}Step 2: Creating ModelConfig...${NC}"
cat <<EOF | kubectl apply -f -
apiVersion: kagent.dev/v1alpha2
kind: ModelConfig
metadata:
  name: $MODELCONFIG_NAME
  namespace: $NAMESPACE
spec:
  provider: SAPAICore
  model: "gpt-4"
  apiKeySecret: $SECRET_NAME
  apiKeySecretKey: SAP_AI_CORE_API_KEY
  sapAICore:
    baseUrl: "$SAP_AI_CORE_BASE_URL"
    resourceGroup: "$SAP_AI_CORE_RESOURCE_GROUP"
    deploymentId: "$SAP_AI_CORE_DEPLOYMENT_ID"
    temperature: 0.7
    maxTokens: 100
EOF
echo -e "${GREEN}✓ ModelConfig created${NC}"
echo ""

# Step 3: Wait for ModelConfig to be ready
echo -e "${YELLOW}Step 3: Waiting for ModelConfig to be accepted...${NC}"
timeout=60
elapsed=0
while [ $elapsed -lt $timeout ]; do
    status=$(kubectl get modelconfig $MODELCONFIG_NAME -n $NAMESPACE -o jsonpath='{.status.conditions[?(@.type=="Accepted")].status}' 2>/dev/null || echo "")
    if [ "$status" = "True" ]; then
        echo -e "${GREEN}✓ ModelConfig is ready${NC}"
        break
    fi
    sleep 2
    elapsed=$((elapsed + 2))
done

if [ $elapsed -ge $timeout ]; then
    echo -e "${RED}✗ Timeout waiting for ModelConfig${NC}"
    exit 1
fi
echo ""

# Step 4: Create Agent
echo -e "${YELLOW}Step 4: Creating Agent...${NC}"
cat <<EOF | kubectl apply -f -
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: $AGENT_NAME
  namespace: $NAMESPACE
spec:
  type: Declarative
  description: "Test agent for SAP AI Core integration"
  declarative:
    modelConfig: $MODELCONFIG_NAME
    systemMessage: "You are a helpful assistant. Keep your responses concise."
    tools: []
EOF
echo -e "${GREEN}✓ Agent created${NC}"
echo ""

# Step 5: Wait for Agent deployment
echo -e "${YELLOW}Step 5: Waiting for Agent deployment...${NC}"
kubectl wait --for=condition=Available \
    deployment/$AGENT_NAME \
    --namespace=$NAMESPACE \
    --timeout=120s || {
    echo -e "${RED}✗ Agent deployment failed${NC}"
    kubectl get pods -n $NAMESPACE -l kagent=$AGENT_NAME
    kubectl logs -n $NAMESPACE -l kagent=$AGENT_NAME --tail=50
    exit 1
}
echo -e "${GREEN}✓ Agent is running${NC}"
echo ""

# Step 6: Test Agent invocation
echo -e "${YELLOW}Step 6: Testing Agent invocation...${NC}"
TEST_MESSAGE="Hello, can you confirm you're working with SAP AI Core?"

# Create a test session
SESSION_RESPONSE=$(curl -s -X POST \
    "http://kagent-controller.$NAMESPACE:8083/api/agents/$NAMESPACE/$AGENT_NAME/sessions" \
    -H "Content-Type: application/json" \
    -d '{"name":"test-session"}')

SESSION_ID=$(echo $SESSION_RESPONSE | jq -r '.data.id')

if [ -z "$SESSION_ID" ] || [ "$SESSION_ID" = "null" ]; then
    echo -e "${RED}✗ Failed to create session${NC}"
    echo "Response: $SESSION_RESPONSE"
    exit 1
fi

echo "Session ID: $SESSION_ID"

# Invoke the agent
INVOKE_RESPONSE=$(curl -s -X POST \
    "http://kagent-controller.$NAMESPACE:8083/api/agents/$NAMESPACE/$AGENT_NAME/invoke" \
    -H "Content-Type: application/json" \
    -d "{
        \"message\": {
            \"role\": \"user\",
            \"contextId\": \"$SESSION_ID\",
            \"parts\": [{\"text\": \"$TEST_MESSAGE\"}]
        }
    }")

# Check if response contains expected fields
if echo "$INVOKE_RESPONSE" | jq -e '.message.parts[0].text' > /dev/null 2>&1; then
    RESPONSE_TEXT=$(echo "$INVOKE_RESPONSE" | jq -r '.message.parts[0].text')
    echo -e "${GREEN}✓ Agent responded successfully${NC}"
    echo "Response: $RESPONSE_TEXT"
else
    echo -e "${RED}✗ Invalid response from agent${NC}"
    echo "Response: $INVOKE_RESPONSE"
    exit 1
fi
echo ""

# Step 7: Check logs
echo -e "${YELLOW}Step 7: Checking agent logs...${NC}"
kubectl logs -n $NAMESPACE -l kagent=$AGENT_NAME --tail=20
echo ""

# Success
echo -e "${GREEN}=== All tests passed! ===${NC}"
echo ""
echo "Summary:"
echo "  • Secret: $SECRET_NAME"
echo "  • ModelConfig: $MODELCONFIG_NAME"
echo "  • Agent: $AGENT_NAME"
echo "  • Session: $SESSION_ID"
echo ""
echo "To clean up resources, run:"
echo "  kubectl delete agent $AGENT_NAME -n $NAMESPACE"
echo "  kubectl delete modelconfig $MODELCONFIG_NAME -n $NAMESPACE"
echo "  kubectl delete secret $SECRET_NAME -n $NAMESPACE"



