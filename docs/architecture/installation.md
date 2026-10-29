# Installation

`go/core/pkg/install` owns installation, prerequisite setup, bundled PostgreSQL, and uninstall orchestration. The CLI resolves configuration and credentials, reports progress, and checks API reachability.

Installation has two phases. Planning resolves chart inputs and the controller service account without changing the cluster. Applying executes the plan in dependency order and stops at the first error. Planning may read values files and fetch charts.

The plan installs CRDs before prerequisites, pod certificates, PostgreSQL, Substrate, and kagent. Chart settings and additional database setup are explicit inputs. Command execution and Kubernetes access are injected so tests can exercise the workflow without external services.

Model API keys reach Helm through temporary values files with `0600` permissions in private directories. The command runner removes them after each invocation and omits failed-command output that could contain credentials. These files override configured values files while caller `--set` overrides retain their priority. Custom runners must honor `Command.SecretValues` with the same protections. Credentials still become part of Helm release data and Kubernetes Secrets.

Uninstall removes resources in the order needed for controllers to finish cleanup. It preserves external databases and the kagent namespace, and it tolerates resources removed by an earlier attempt. A namespace mismatch stops cleanup before deletion.
