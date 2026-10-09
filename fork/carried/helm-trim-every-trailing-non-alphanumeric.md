| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(helm): trim every trailing non-alphanumeric from the truncated label values` | `helm.sh/chart` and `app.kubernetes.io/version` are cut to 63 characters and only a trailing `-` was trimmed; a dev build of a long branch (`0.11.0-dev.fork-repin-substrate-gs5.…`) was cut after a `.` and the apiserver refused every labelled object, so the upgrade failed and rolled back | to file; giantswarm/giantswarm#37742 row 62 |
