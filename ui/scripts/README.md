# Container scripts

`init.sh` is the UI image's entrypoint (`CMD` in [`../Dockerfile`](../Dockerfile)). It
renders the deployment's settings into `env-config.js` from the pod's environment and
then execs nginx, which is what lets one image serve every deployment.

Runtime, not tooling — nothing here runs from a checkout. Developer scripts live in
[`scripts/`](../../scripts) at the repository root.
