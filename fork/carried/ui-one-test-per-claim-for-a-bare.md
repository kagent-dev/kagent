| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `test(ui): one test per claim for a bare build's extension points` | "a bare build renders the application and nothing else" timed out in its last `page.goto` on a loaded Firefox run (giantswarm/kagent-upstream#79): five independent claims, six page loads on the thirty-second budget of one. Four tests of one to three loads each | to file — same branch and row as the line above |
