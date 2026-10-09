| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `ci(fork): follow every controller pod's log through the e2e run` | the e2e job read the controller log from the Deployment once the tests were over, which holds only the final pod; the tests that roll the controller (`withControllerEnv`, the sandbox restart) lost the log of every earlier pod, among them the window of every failure analysed for giantswarm/kagent-upstream#204. The job follows every controller pod from the moment it runs, one file per pod | fork-only |
