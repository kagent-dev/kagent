# The Giant Swarm kagent line

This repository is the kagent the [Giant Swarm Agent Platform](https://github.com/giantswarm/agent-platform)
runs: upstream [kagent-dev/kagent](https://github.com/kagent-dev/kagent) `main` at a pinned commit, plus the
patches the platform cannot run without until they are merged upstream. Team Bumblebee owns it. The line
exists because the platform moved to the kagent API v2 (`kagent.dev/v1alpha3`, Substrate actors) before
upstream has released it; the day upstream ships a release that contains every carried patch, the platform
switches to that release and this line ends.

| | |
|---|---|
| Consumed branch (and default branch) | `poc/agent-platform` — what the platform's dev channel, agentlab and the release tags follow |
| Mirror | `main`, every `release/v*.x` branch and every tag — read-only mirrors of upstream, fast-forwarded by the sync workflow, never edited (upstream's CI reads release branches and tags to pick the upgrade-test baseline) |
| Automation branches | `sync/**` (re-pin candidates), `ledger` (machine-written records) — never commit to them by hand |
| Upstream pin | `git merge-base poc/agent-platform main` — the newest row of [`re-pins.md`](../../blob/ledger/re-pins.md) on the `ledger` branch gives the commit and date |
| Substrate version the pin runs with | `0.0.27-dev.giantswarm.2026-09-10.19-33-37.h734ec53` of the Giant Swarm line [giantswarm/substrate](https://github.com/giantswarm/substrate) (upstream kagent-dev/substrate v0.0.26 — the `go/go.mod` replace, the ate-api contract — plus kagent-dev/substrate#33; `SUBSTRATE_VERSION` / `SUBSTRATE_REPO` in the `Makefile`, read by `ci.yaml` through `make substrate-pin`; `kubectl-ate` for the e2e bootstrap still from upstream's v0.0.26 release) |
| Tracking | giantswarm/giantswarm#37010 (the line), giantswarm/giantswarm#37742 (the upstream exit of every patch) |

## Which kagent are we running

Carried commits, in the order they sit on top of the pin (`git log --oneline main..poc/agent-platform`).
Their SHAs change at every re-pin; the ledger records the SHAs of each rebase.

| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(controller): accept a RemoteMCPServer labeled kagent.dev/discovery=disabled without listing its tools` | the per-agent muster `RemoteMCPServer` carries a toolset header and must not be tool-listed by the controller, which has no bearer | kagent-dev/kagent#2752 — open |
| `fix(controller): keep a kagent.dev/discovery=disabled MCPServer out of the catalog` | the kmcp `MCPServer` half of the same opt-out | kagent-dev/kagent#2752 — open |
| `fix(harness): re-read the continuation state from /data on every turn` | the Claude and Codex harnesses cache the continuation file at process start while the actor resumes from the golden process image plus the data snapshot, so every turn forgot the conversation | to file |
| `fix(helm): sanitise the chart version in the app.kubernetes.io/version label` | helm-controller installs the chart as `<version>+<digest>`, which is not a valid label value | to file |
| `Use apk upgrade on harness images (#2756)` | Trivy: fixable OpenSSL HIGH (CVE-2026-14456) in the Alpine layer of the Claude harness image | kagent-dev/kagent#2756 — **merged** as `8e1ff48f`, carried as a cherry-pick until the next re-pin drops it |
| `fix(docker): apk upgrade in the Go runtime image` | the same Alpine finding in the controller and Go ADK images (`go/Dockerfile`), which #2756 did not cover | to file (the harness half is upstream's own pattern) |
| `ci(fork): publish dev images and charts to ghcr.io/<owner> from the poc branch` | the fork's `tag.yaml` (below) | fork-only, never upstream |
| `ci(fork): retry image builds and keep the matrix running on a single failure` | hosted-runner flakiness of `apk.cgr.dev` | fork-only |
| `ci(fork): test and scan the consumed branch, re-pin weekly by workflow, keep a ledger` | this document, `ci.yaml`/`image-scan.yaml` on the consumed branch, `sync-upstream.yaml`, the ledger | fork-only |

Rules for the table: every non-`ci(fork)` row has an upstream pull request or a "to file" that
giantswarm/giantswarm#37742 tracks; a row leaves when the sync drops the commit because upstream merged it
(the re-pin report names the upstream commit) — remove the row in a follow-up pull request. Giant Swarm-specific
wiring (Substrate settings, the platform `Harness` objects, pgvector, registries) lives in the agent-platform
charts, not here.

## What is published

`.github/workflows/tag.yaml` (fork-rewritten) runs on every push to the consumed branch and on `v*.*.*` tags:

| Artifact | Reference |
|---|---|
| Images (multi-arch, amd64 + arm64) | `ghcr.io/giantswarm/kagent/{controller,ui,golang-adk,claude-harness}:<version>` |
| Charts | `oci://ghcr.io/giantswarm/kagent/helm/kagent:<version>`, `oci://ghcr.io/giantswarm/kagent/helm/kagent-crds:<version>` (the chart's image defaults are stamped with the same version) |
| Dev version of a branch push | `0.11.0-dev.poc-agent-platform.<YYYY-MM-DD>.<HH-MM-SS>.h<sha7>` — base `0.11.0` (upstream `main`'s next release), the branch lowercased to `[a-z0-9-]`, the committer date in UTC, the short commit. Consumers select the channel with a Flux `semverFilter` of `.*-dev\.poc-agent-platform\..*` on the range `>=0.11.0-0 <0.12.0-0` |
| Release version of a tag | the tag without `v` |
| Digests | every build's image and chart digests (multi-arch index digests, what a `Harness` pins) are a row of [`builds.md`](../../blob/ledger/builds.md) on the `ledger` branch and in the run summary of the workflow run |

Only what the platform consumes is published: the Python ADK, the Codex harness and the CLI image are not
offered on the platform and are not built here. Consumers of the dev builds: the agent-platform meta chart's
dev line (`components.kagent` follows the channel; its `kagent.tag` and the two `Harness` image digests are pinned
there and re-pinned per build), agentlab (`agentlab configure --chart-branch poc/kagent-main`), and the test
cluster following the channel.

Publishing does not wait for the tests. Nothing reaches the consumed branch untested — a pull request needs a
green `ci-ok` and `scan-ok` on an up-to-date branch, and the sync lands a candidate only after both passed on
its exact head (the same commits it then force-pushes) — so the push-triggered run on the consumed branch is a
repetition; a red one is a flake to re-run or an environment drift to fix, visible on the commit and in the
Actions tab, never a reason to pull a build that was green on the same tree.

## CI and security

`ci.yaml` (upstream's suite: Go unit tests and lint, Helm unit tests, protobuf contract check, manifests check,
UI typecheck/lint/unit/browser tests, Python tests and lint, multi-arch image builds, the e2e against kind +
Substrate, the upgrade legs) and `image-scan.yaml` (Trivy on every published image at CRITICAL/HIGH,
`govulncheck` over the Go module graph) run on every push to `poc/agent-platform` and `sync/**`, on every pull
request to `poc/agent-platform`, and — the scan — weekly. Fork differences from upstream's files: the triggers,
GitHub-hosted runners with a `docker/setup-buildx-action` builder instead of Blacksmith runners and their
builder, the image matrices trimmed to the published set, no `paths-ignore` (a docs-only pull request must still
report the required checks), and one aggregate job per workflow — `ci-ok`, `scan-ok` — which the ruleset requires.

Security findings are handled by the team's policy: a fixable finding is fixed (a dependency bump is a patch
like any other — upstream pull request first, carried here until merged; an image fix goes into the Dockerfile
the same way, as the two `apk upgrade` commits above). A finding upstream cannot fix yet, or a scanner
false positive, gets a time-boxed, justified ignore — `.trivyignore` with Trivy's native `exp:YYYY-MM-DD` and a
comment naming the module and the reason — so the line keeps moving; it is re-reviewed at the expiry. `govulncheck`
has no ignore file of its own, so `scripts/fork/govulncheck.sh` applies `.govulncheck-ignore` (`GO-… until=… # why`)
the same way: an expired entry fails the scan until it is re-reviewed, and only a finding with no fixed version
may be listed there.

Upstream automation the fork does not need is off: Dependabot version updates are disabled on this fork (a
fork's default; dependency updates arrive through the re-pin), `stalebot`, `conventional-label` and
`label-pull-requests` are disabled workflows (repository setting, not a file edit), `migration-immutability`
only fires on pull requests to `main`/`release/**`, which do not exist here. Upstream's workflow files do start
when the mirror is pushed (the push has to be made as the App — see "The mirror"); the sync cancels those runs
within seconds, so a cancelled run per mirrored branch or tag in the Actions tab is expected, not a failure.

## Re-pin

The line is rebased onto upstream `main` weekly (Monday 05:00 UTC) by `.github/workflows/sync-upstream.yaml`,
also on demand (`gh workflow run sync-upstream.yaml`, inputs `upstream_ref`, `dry_run`). One run:

1. fast-forwards the mirror — `main`, the `release/v*.x` branches, the tags — to upstream (`scripts/fork/mirror.sh`;
   refuses if a mirrored branch was edited; never moves or deletes a ref) and cancels the runs upstream's own
   workflow files start at the mirrored refs (`scripts/fork/mirror-quiet.sh`);
2. rebases the carried commits onto the upstream head into a candidate branch `sync/upstream-<date>-<sha7>`
   (`scripts/fork/repin.sh`; commits upstream merged in the meantime are dropped and named in the report,
   with the upstream commit they landed as);
3. pushes the candidate (as the org's `heraldbot` GitHub App, whose token the run mints — a push by an App
   triggers workflows, a push with the workflow's own token would not); `ci.yaml` and `image-scan.yaml` run on it;
   the workflow waits for `ci-ok` and `scan-ok`;
4. green: force-pushes the candidate onto `poc/agent-platform` (`--force-with-lease` on the head it started
   from), deletes the candidate, appends the row to `re-pins.md`; `tag.yaml` publishes the dev build and
   appends its row to `builds.md`;
5. a conflict or a red check: nothing is pushed to the consumed branch; a pull request against
   `poc/agent-platform` names the conflicting carried commit (with the files) or the failed checks, and the
   candidate branch stays for the fix. Finish by hand, then close that pull request — never merge it (a merge
   would keep the old base).

A re-pin is proven before anything depends on it: the agent-platform dev line re-pins the new build's tag and
`Harness` digests, and agentlab on the dev channel runs its proofs from the published artifacts.

### Manual re-pin

The same steps by hand, for when the workflow is broken or a conflict needs a human:

```bash
git clone https://github.com/giantswarm/kagent-upstream && cd kagent-upstream
git remote add upstream https://github.com/kagent-dev/kagent.git
CANDIDATE=sync/upstream-$(date -u +%Y%m%d)-manual scripts/fork/repin.sh   # exit 2 = conflict, report in repin-report.md
# on a conflict: git rebase --onto upstream/main "$(git merge-base origin/poc/agent-platform upstream/main)" ; fix ; git rebase --continue
git push origin "$CANDIDATE"                      # ci.yaml + image-scan.yaml run on sync/** ; wait for ci-ok and scan-ok
git push --force-with-lease=refs/heads/poc/agent-platform origin "$CANDIDATE":poc/agent-platform
git push origin --delete "$CANDIDATE"
scripts/fork/mirror.sh "$(gh auth token)" && scripts/fork/mirror-quiet.sh "<ISO time before the push>" mirror-pushed.txt   # or: gh workflow run sync-upstream.yaml -f mirror_only=true
scripts/fork/ledger-append.sh re-pins.md '| <date> | <old pin> | <new pin> | <old head> → <new head> | <replayed> | <dropped> | manual |'
```

The force-push needs a bypass of the consumed branch's ruleset, which only the automation (the `heraldbot`
App) has: an administrator adds themselves to the bypass list for the duration and removes themselves right
after (the same discipline as lifting `enforce_admins` for a guarded merge). Never `--force` without
`--force-with-lease`.

### The mirror

`main`, the `release/v*.x` branches and the tags are fast-forwarded by the sync as the App (`scripts/fork/mirror.sh`;
`protect-mirror-main` ruleset on `main`: no deletion, no force-push). The workflow's own token cannot be used:
GitHub refuses any push that creates or updates a file under `.github/workflows` without the `workflows`
permission, mirrored upstream commits included, and `GITHUB_TOKEN` never has it. An App push starts upstream's
workflow files at the mirrored refs; `scripts/fork/mirror-quiet.sh` cancels them right after. Upstream's CI
resolves the upgrade-test baseline from the fork's own release branches and tags, so the mirror must carry them
(the fork's own release tags, when they come, need a version scheme that cannot collide with upstream's `v*`).
If a mirrored branch was ever edited, the sync refuses to fast-forward it; restore it with
`git push --force-with-lease=refs/heads/<branch> origin upstream/<branch>:<branch>` after lifting the ruleset
briefly, then run the sync again. `gh workflow run sync-upstream.yaml -f mirror_only=true` mirrors without
re-pinning.

## Branch protection

Ruleset `protect-consumed-branch` on the default branch: no deletion, no force-push, changes only through a
pull request with one approval, required status checks `ci-ok` and `scan-ok` on an up-to-date branch. Pull
requests are **rebase-merged, never squashed**: every commit on the consumed branch is one carried patch with
its own upstream exit, and the re-pin drops a commit only when its patch matches an upstream commit — a squash
would weld a cherry-picked upstream fix to unrelated changes and lose that.
Bypass: the `heraldbot` GitHub App (the org's automation App, installed on every repository; the sync
pushes with a token it mints from the org secrets) always — that is the re-pin's force-push; repository
administrators for pull requests only (merging an own green pull request without waiting for the approval).
Ruleset `protect-mirror-main` on `main`: no deletion, no force-push. Teams `team-bumblebee` and `bots` have
write access.

## Contributing

- **Every patch is an upstream pull request first** (kagent-dev/kagent, DCO sign-off on every commit; a
  workflow run on a pull request from a fork waits for a maintainer's approval). The fork commit and the
  upstream pull request are the same change and reference each other. Work on a branch of this repository
  (`fix/…`, `feat/…`) and open the pull request here against `poc/agent-platform` with the upstream link; the
  upstream pull request is opened from this repository's branch too, so the same commit serves both.
- **A personal fork** is for exploration that may never be proposed; the moment a change is meant for the
  platform it moves here.
- **A patch lives here until upstream merges it**; the next re-pin drops it. A patch upstream rejects is
  either re-proposed in the shape the maintainers want or recorded in the table above as a deliberate
  permanent difference with the reason — the target is none.
- Anything Giant Swarm-specific (Substrate wiring, platform `Harness` objects, registries, security contexts)
  belongs to the agent-platform charts, not to this line.
- Never commit to `main` (the mirror), `sync/**` or `ledger`.

## Ledger

Machine-written, on the `ledger` branch: [`builds.md`](../../blob/ledger/builds.md) — one row per published
build (version, commit, upstream pin, every image and chart digest, the run); [`re-pins.md`](../../blob/ledger/re-pins.md)
— one row per re-pin (previous and new pin, old and new head, replayed and dropped commits, the run).
Human-written, here: the tables above. Together they answer "which kagent are we running" without opening an
issue tracker; giantswarm/giantswarm#37010 and #37742 hold the wider context.
