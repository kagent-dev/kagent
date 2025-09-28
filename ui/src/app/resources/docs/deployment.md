# Deployment Guide

## Prerequisites

### System Requirements
- **CPU**: 2+ cores (4+ recommended)
- **Memory**: 4GB+ RAM (8GB+ recommended)
- **Storage**: 20GB+ free space
- **OS**: Linux/macOS/Windows (Linux recommended for production)

### Required Software
- Docker 20.10+
- Docker Compose 2.0+
- kubectl (for Kubernetes deployment)
- Helm (for Kubernetes deployment)

## Deployment Options

### 1. Docker Compose (Development/Staging)

#### Configuration
Create a `.env` file:
```env
# Database
POSTGRES_USER=adolphe
POSTGRES_PASSWORD=securepassword
POSTGRES_DB=adolphe
DATABASE_URL=postgresql://adolphe:securepassword@db:5432/adolphe

# Redis
REDIS_URL=redis://redis:6379

# Authentication
NEXTAUTH_SECRET=your-secret-key
NEXTAUTH_URL=http://localhost:3000

# AI Providers
OPENAI_API_KEY=your-openai-key
```

#### Start Services
```bash
docker-compose up -d
```

### 2. Kubernetes (Production)

#### Prerequisites
- Kubernetes cluster (v1.22+)
- Ingress controller (e.g., Nginx Ingress)
- Cert-manager (for TLS certificates)

#### Install with Helm
1. Add the Helm repository:
   ```bash
   helm repo add adolphe https://charts.adolphe.ai
   helm repo update
   ```

2. Create a `values.yaml`:
   ```yaml
   replicaCount: 3
   
   image:
     repository: adolphe/ai
     tag: latest
     pullPolicy: IfNotPresent
   
   ingress:
     enabled: true
     className: nginx
     hosts:
       - host: ai.yourdomain.com
         paths:
           - path: /
             pathType: Prefix
     tls:
       - secretName: adolphe-tls
         hosts:
           - ai.yourdomain.com
   
   database:
     enabled: true
     postgresql:
       auth:
         username: adolphe
         password: securepassword
         database: adolphe
   
   redis:
     enabled: true
   ```

3. Install the chart:
   ```bash
   helm install adolphe adolphe/adolphe-ai -f values.yaml
   ```

## Configuration

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `NODE_ENV` | No | `production` | Runtime environment |
| `PORT` | No | `3000` | Application port |
| `DATABASE_URL` | Yes | - | PostgreSQL connection string |
| `REDIS_URL` | Yes | - | Redis connection URL |
| `NEXTAUTH_SECRET` | Yes | - | Secret for session encryption |
| `NEXTAUTH_URL` | Yes | - | Base URL of your application |
| `OPENAI_API_KEY` | No | - | OpenAI API key |
| `ANTHROPIC_API_KEY` | No | - | Anthropic API key |

### Scaling

#### Horizontal Pod Autoscaling
```yaml
# hpa.yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: adolphe-web
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: adolphe-web
  minReplicas: 3
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
```

## Monitoring

### Prometheus Metrics
Metrics are exposed at `/metrics` endpoint. To enable Prometheus monitoring:

1. Install Prometheus Operator:
   ```bash
   kubectl apply -f https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/master/bundle.yaml
   ```

2. Create a ServiceMonitor:
   ```yaml
   # servicemonitor.yaml
   apiVersion: monitoring.coreos.com/v1
   kind: ServiceMonitor
   metadata:
     name: adolphe-monitor
     labels:
       app: adolphe
   spec:
     selector:
       matchLabels:
         app: adolphe
     endpoints:
     - port: web
       path: /metrics
   ```

## Backup and Recovery

### Database Backups
```bash
# Create backup
pg_dump -h localhost -U adolphe -F c -b -v -f backup.dump adolphe

# Restore from backup
pg_restore -h localhost -U adolphe -d adolphe -v backup.dump
```

### Automated Backups with Kubernetes
```yaml
# backup-cronjob.yaml
apiVersion: batch/v1beta1
kind: CronJob
metadata:
  name: db-backup
spec:
  schedule: "0 0 * * *"
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: backup
            image: postgres:13
            command: ["sh", "-c"]
            args:
              - |
                PGPASSWORD=$POSTGRES_PASSWORD pg_dump -h $POSTGRES_HOST -U $POSTGRES_USER -d $POSTGRES_DB -f /backup/backup-$(date +%Y%m%d).sql
            env:
            - name: POSTGRES_HOST
              value: postgres
            - name: POSTGRES_USER
              valueFrom:
                secretKeyRef:
                  name: postgres-secrets
                  key: username
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: postgres-secrets
                  key: password
            - name: POSTGRES_DB
              value: adolphe
            volumeMounts:
            - name: backup-volume
              mountPath: /backup
          restartPolicy: OnFailure
          volumes:
          - name: backup-volume
            persistentVolumeClaim:
              claimName: backup-pvc
```

## Upgrading

### Version Compatibility
Always check the [release notes](https://github.com/your-org/adolphe-ai/releases) for upgrade instructions specific to each version.

### Upgrade Process
1. Backup your database
2. Update the Docker image or Helm chart version
3. Apply migrations (if any)
4. Restart services

## Troubleshooting

### Common Issues

#### Database Connection Issues
```bash
# Check database connectivity
psql $DATABASE_URL -c "SELECT 1"

# Check database logs
docker-compose logs db
```

#### High CPU/Memory Usage
```bash
# Check resource usage
kubectl top pods

# Get pod metrics
kubectl describe pod <pod-name>
```

#### Application Logs
```bash
# View logs
docker-compose logs -f web

# Or for Kubernetes
kubectl logs -f deployment/adolphe-web
```

## Support
For additional help, please contact our support team at support@adolphe.ai or open an issue on our [GitHub repository](https://github.com/your-org/adolphe-ai/issues).
