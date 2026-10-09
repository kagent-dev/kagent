| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `ci(fork): keep the browser suite's evidence of a failed run` | `ui-tests` uploads `ui/test-results` as a seven-day artifact when the job fails: the screenshot and video of every failed attempt, the trace of every retry. The log names the step that timed out; only the trace shows what was on the page and under the pointer while it did, and the hosted runner discarded it with the workspace (giantswarm/kagent-upstream#68 had to be diagnosed from a local reproduction for that reason) | fork-only |
