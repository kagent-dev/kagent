| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(harness): keep the compiled skills on Claude turns that need approval` | Claude Code counts skills under an `--add-dir` directory as part of the project setting source, so the `--setting-sources ""` the driver passed on approval turns dropped every compiled skill (Claude Code 2.1.260); the driver now passes only `--settings` and `--permission-prompt-tool`, and the rendered `ask` rules still hold over user and project `allow` rules (bumblebee-plans#60, D5) | to file |
