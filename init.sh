export DOCKER_API_VERSION=1.43
export OPENAI_API_KEY=fake

make helm-install

HOST_IP=$(docker network inspect kind -f '{{range .IPAM.Config}}{{if .Gateway}}{{.Gateway}}{{"\n"}}{{end}}{{end}}' | grep -E '^[0-9]+\.' | head -1)
echo "Detected Kind network gateway: $HOST_IP"
export KAGENT_LOCAL_HOST=$HOST_IP

export KAGENT_URL="http://$(kubectl get svc -n kagent kagent-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'):8083"
echo "KAGENT_URL: $KAGENT_URL"
echo "KAGENT_LOCAL_HOST: $KAGENT_LOCAL_HOST"
