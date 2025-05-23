export CLUSTER_CTX=kind-kagent
docker pull mysql:9.2.0 || true
kind load docker-image mysql:9.2.0 --name kagent || true

kubectl apply --context "${CLUSTER_CTX}" -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: mysql-v1
  namespace: default
  labels:
    app: mysql
spec:
  ports:
  - port: 3306
    protocol: TCP
    targetPort: 3306
  selector:
    app: mysql
    version: v1
  type: ClusterIP
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: mysql-pvc
  namespace: default
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
  storageClassName: standard
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mysql-v1
  namespace: default
  labels:
    app: mysql
    version: v1
spec:
  replicas: 1
  selector:
    matchLabels:
      app: mysql
      version: v1
  template:
    metadata:
      labels:
        app: mysql
        version: v1
    spec:
      containers:
      - name: mysql
        image: mysql:9.2.0
        ports:
          - name: http
            containerPort: 3306
            protocol: TCP
        volumeMounts:
        - name: mysql-persistent-storage
          mountPath: /var/lib/mysql
        livenessProbe:
          tcpSocket:
            port: http
        readinessProbe:
          tcpSocket:
            port: http
        env:
        - name: "MYSQL_ROOT_PASSWORD"
          value: "password"
        - name: "MYSQL_DATABASE"
          value: "demo"
      volumes:
      - name: mysql-persistent-storage
        persistentVolumeClaim:
          claimName: mysql-pvc
EOF

docker pull bitnami/neo4j:5.26.1 || true
kind load docker-image bitnami/neo4j:5.26.1 --name kagent || true

kubectl apply --context "${CLUSTER_CTX}" -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: neo4j-db-v1
  namespace: default
  labels:
    app: neo4j-db
spec:
  ports:
  - port: 7474
    protocol: TCP
    targetPort: 7474
  selector:
    app: neo4j-db
    version: v1
  type: ClusterIP
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: neo4j-db-v1
  namespace: default
  labels:
    app: neo4j-db
    version: v1
spec:
  replicas: 1
  selector:
    matchLabels:
      app: neo4j-db
      version: v1
  template:
    metadata:
      labels:
        app: neo4j-db
        version: v1
    spec:
      containers:
      - name: neo4j-db
        image: bitnami/neo4j:5.26.1
        ports:
          - name: http
            containerPort: 7474
            protocol: TCP
        livenessProbe:
          tcpSocket:
            port: http
        readinessProbe:
          tcpSocket:
            port: http
        env:
        - name: "NEO4J_PASSWORD"
          value: "password"
EOF

docker pull nicholasjackson/fake-service:v0.26.2 || true
kind load docker-image nicholasjackson/fake-service:v0.26.2 --name kagent || true
kubectl apply --context "${CLUSTER_CTX}" -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: backend-v1
  namespace: default
  labels:
    app: backend
spec:
  ports:
  - port: 8080
    protocol: TCP
    targetPort: 8080
  selector:
    app: backend
    version: v1
  type: ClusterIP
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: backend-v1
  namespace: default
  labels:
    app: backend
    version: v1
spec:
  replicas: 1
  selector:
    matchLabels:
      app: backend
      version: v1
  template:
    metadata:
      labels:
        app: backend
        version: v1
    spec:
      containers:
      - name: backend
        image: nicholasjackson/fake-service:v0.26.2
        ports:
          - name: http
            containerPort: 8080
            protocol: TCP
        livenessProbe:
          tcpSocket:
            port: http
        readinessProbe:
          tcpSocket:
            port: http
        env:
        - name: "LISTEN_ADDR"
          value: "0.0.0.0:8080"
        - name: "NAME"
          value: "backend-v1"
        - name: "MESSAGE"
          value: "Hello From backend (v1)!"
EOF

kubectl apply --context "${CLUSTER_CTX}" -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: backend-v2
  namespace: default
  labels:
    app: backend
spec:
  ports:
  - port: 8080
    protocol: TCP
    targetPort: 8080
  selector:
    app: backend
    version: v2
  type: ClusterIP
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: backend-v2
  namespace: default
  labels:
    app: backend
    version: v2
spec:
  replicas: 1
  selector:
    matchLabels:
      app: backend
      version: v2
  template:
    metadata:
      labels:
        app: backend
        version: v2
    spec:
      containers:
      - name: backend
        image: nicholasjackson/fake-service:v0.26.2
        ports:
          - name: http
            containerPort: 8080
            protocol: TCP
        livenessProbe:
          tcpSocket:
            port: http
        readinessProbe:
          tcpSocket:
            port: http
        env:
        - name: "LISTEN_ADDR"
          value: "0.0.0.0:8080"
        - name: "NAME"
          value: "backend-v2"
        - name: "MESSAGE"
          value: "Hello From backend (v2)!"
        - name: "UPSTREAM_URIS"
          value: "http://neo4j-db-v1:7474"
EOF

docker pull node:23-alpine || true
kind load docker-image node:23-alpine --name kagent || true

kubectl apply --context "${CLUSTER_CTX}" -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: backend-v3-code
  namespace: default
data:
  index.js: |
    const express = require('express');
    const mysql = require('mysql2/promise');
    const app = express();

    const pool = mysql.createPool({
      host: process.env.MYSQL_HOST,
      user: process.env.MYSQL_USER,
      password: process.env.MYSQL_PASSWORD,
      database: process.env.MYSQL_DATABASE,
      waitForConnections: true,
    });

    // Initialize database and table
    async function initDB() {
      try {
        const connection = await pool.getConnection();
        await connection.execute(\`
          CREATE TABLE IF NOT EXISTS messages (
            id INTEGER PRIMARY KEY AUTO_INCREMENT,
            message VARCHAR(255),
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
          ) ENGINE=InnoDB;
        \`);
        await connection.execute("INSERT INTO messages (message) VALUES ('Hello from backend-v3!')");
        connection.release();
      } catch (err) {
        console.error('Database initialization error:', err);
        process.exit(1);
      }
    }

    // Initialize DB on startup
    initDB();
    console.log('Database initialized');

    app.get('/', async (req, res) => {
      try {
        console.log(\`Received request: \${req.method} \${req.url}\`);
        await initDB();
        const [rows] = await pool.execute('SELECT * FROM messages ORDER BY created_at DESC LIMIT 1');
        res.json({
          service: "backend-v3",
          database: "connected",
          lastMessage: rows[0]
        });
      } catch (err) {
        res.status(500).json({
          service: "backend-v3",
          database: "error",
          error: err.message
        });
      }
    });

    app.listen(8080, () => {
      console.log("backend v3 listening at http://localhost:8080");
    });
---
apiVersion: v1
kind: Service
metadata:
  name: backend-v3
  namespace: default
  labels:
    app: backend
spec:
  ports:
  - port: 8080
    protocol: TCP
    targetPort: 8080
  selector:
    app: backend
    version: v3
  type: ClusterIP
---
apiVersion: v1
kind: Secret
metadata:
  name: mysql-secrets
  namespace: default
type: Opaque
stringData:
  MYSQL_HOST: mysql-v1
  MYSQL_USER: root
  MYSQL_PASSWORD: password
  MYSQL_DATABASE: demo
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: backend-v3
  namespace: default
  labels:
    app: backend
    version: v3
spec:
  replicas: 1
  selector:
    matchLabels:
      app: backend
      version: v3
  template:
    metadata:
      labels:
        app: backend
        version: v3
    spec:
      containers:
      - name: backend
        image: node:23-alpine
        ports:
          - name: http
            containerPort: 8080
            protocol: TCP
        livenessProbe:
          tcpSocket:
            port: http
        readinessProbe:
          tcpSocket:
            port: http
        volumeMounts:
        - name: backend-v3-code
          mountPath: /app/index.js
          subPath: index.js
        command: ["/bin/sh"]
        args:
        - -c
        - |
          apk add --no-cache nodejs npm mysql-client
          cd /app
          npm init -y
          npm install express mysql2
          node index.js
        envFrom:
        - secretRef:
            name: mysql-secrets
      volumes:
      - name: backend-v3-code
        configMap:
          name: backend-v3-code
EOF

kubectl apply --context "${CLUSTER_CTX}" -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: frontend-v1
  namespace: default
  labels:
    app: frontend
spec:
  ports:
  - port: 8080
    protocol: TCP
    targetPort: 8080
  selector:
    app: frontend
    version: v1
  type: NodePort
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: frontend-v1
  namespace: default
  labels:
    app: frontend
    version: v1
spec:
  replicas: 1
  selector:
    matchLabels:
      app: frontend
      version: v1
  template:
    metadata:
      labels:
        app: frontend
        version: v1
    spec:
      containers:
      - name: frontend
        image: nicholasjackson/fake-service:v0.26.2
        ports:
          - name: http
            containerPort: 8080
            protocol: TCP
        livenessProbe:
          tcpSocket:
            port: http
        readinessProbe:
          tcpSocket:
            port: http
        env:
        - name: "LISTEN_ADDR"
          value: "0.0.0.0:8080"
        - name: "NAME"
          value: "frontend-v1"
        - name: "MESSAGE"
          value: "Hello From frontend (v1)!"
        - name: "UPSTREAM_URIS"
          value: "backend-v1:8080,http://backend-v2:8080,http://backend-v3:8080"
EOF
kubectl --context ${CLUSTER_CTX} rollout status deployment
