| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `test(ui): wait for pressUntil's control before pressing it` | `pressUntil` skipped a first attempt whose control was not drawn yet and then waited out the settled assertion's five seconds: traced on the schedules lifecycle's "Weekly" option in two of three loaded Firefox runs, part of what took the journey past its sixty-second budget (giantswarm/kagent-upstream#79). It waits for the control first, as `pressOnce` does | to file — same branch and row as the line above |
