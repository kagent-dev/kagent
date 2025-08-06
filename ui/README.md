# kagents ui


```bash
# install the dependencies
npm install

# run the frontend
npm run dev
```

# Testing the UI against a k8s backend

Often during UI development, you'll want to test the UI against a k8s backend.

To do this, you can follow the instructions in the [DEVELOPMENT.md](../DEVELOPMENT.md) file.

Once you have kagent running, you'll need to port-forward the kagent-controller service to your local machine.

```bash
kubectl port-forward svc/kagent-controller 8083
```

Once you have the backend running, you can run the UI with the following command:

```bash
npm run dev
```

This will start the UI and connect to the backend running in the kind cluster.