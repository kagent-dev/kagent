# The Giant Swarm kagent line

This repository is the kagent the [Giant Swarm Agent Platform](https://github.com/giantswarm/agent-platform)
runs: upstream [kagent-dev/kagent](https://github.com/kagent-dev/kagent) `main` at a pinned commit, plus the
patches the platform cannot run without until they are merged upstream. Team Bumblebee owns it. The line
exists because the platform moved to the kagent API v2 (`kagent.dev/v1alpha3`, Substrate actors) before
upstream has released it; the day upstream ships a release that contains every carried patch, the platform
switches to that release and this line ends.

| | |
|---|---|
| Consumed branch (and default branch) | `giantswarm` (the same name as the Substrate line's) — what the release tags are cut from, and what the platform's dev channel and agentlab follow between releases |
| Mirror | `main`, every `release/v*.x` branch and every tag — read-only mirrors of upstream, fast-forwarded by the sync workflow, never edited (upstream's CI reads release branches and tags to pick the upgrade-test baseline) |
| Automation branches | `sync/**` (re-pin candidates), `ledger` (machine-written records) — never commit to them by hand |
| Upstream pin | `git merge-base giantswarm main` — the newest row of [`re-pins.md`](../../blob/ledger/re-pins.md) on the `ledger` branch gives the commit and date |
| Substrate version the pin runs with | `0.0.27-gs.6` of the Giant Swarm line [giantswarm/substrate](https://github.com/giantswarm/substrate) — its release `v0.0.27-gs.6`: upstream kagent-dev/substrate v0.0.26 (the `go/go.mod` replace, the ate-api contract) plus the line's carried patches, kagent-dev/substrate#33 among them; the Substrate the platform's meta chart runs, whose worker `ghcr.io/giantswarm/substrate/ateom-gvisor:0.0.27-gs.6` the published chart stamps into `substrateWorkerPool.workerImage` (`SUBSTRATE_VERSION` / `SUBSTRATE_REPO` in the `Makefile`, read by `ci.yaml` through `make substrate-pin` and by `tag.yaml` at publish; `kubectl-ate` for the e2e bootstrap still from upstream's v0.0.26 release) |
| Tracking | giantswarm/giantswarm#37010 (the line), giantswarm/giantswarm#37742 (the upstream exit of every patch) |

## Which kagent are we running

Carried commits, in the order they sit on top of the pin (`git log --oneline main..giantswarm`).
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
| `ci(fork): stamp the golang-adk digest and SUBSTRATE_VERSION into the chart at publish` | the published chart carries the index digests of its own runtime images and the Substrate worker image of the Makefile's pin, so the platform's meta chart stops re-pinning by hand what the line already knows (giantswarm/giantswarm#37765) | fork-only |
| `feat(helm): controller.agentImage.digest and an optional default Harness for the chart's own runtime image` | the platform `Harness` of the Go ADK runtime renders from the chart's own digest (`harness.create`, off by default) instead of a digest copied into the meta chart | to file — kagent-dev/kagent#2529 is the open bug (no build-time digest), kagent-dev/kagent#2536 (closed, unmerged) proposed the key this patch uses |
| `feat(helm): a ConfigMap of the chart's runtime image references for the Substrate image cache` | the pinned-image set of Substrate's image cache follows the chart's digests instead of a third copy in the meta chart | to file, queued behind the Substrate `pinnedImages` flag it feeds |
| `fix(controller): honour AUTH_MODE and wire the trusted-proxy authenticator` | the platform fronts the controller with agentgateway, which validates the caller's JWT and sets the identity header from its `email` claim; the controller re-derives the identity from the bearer (`controller.auth.mode: trusted-proxy`, `controller.auth.userIdClaim: email`) instead of trusting an `X-User-Id` from whoever reaches it. On `main` the chart renders `AUTH_MODE` and nothing has read it since kagent-dev/kagent#2595 | to file — prepared in the fork: branch `upstream/trusted-proxy-auth-mode`; giantswarm/giantswarm#37742 row 2 |
| `fix(helm): keep the kagent-crds CRDs on uninstall (helm.sh/resource-policy: keep)` | the `kagent-crds` chart ships the CRDs as plain templates (no `crds/` directory), so an uninstall of that release deleted the five CRDs and every `AgentTemplate`/`RemoteMCPServer` with them; the meta chart relies on both surviving. Added as a `+kubebuilder:metadata:annotations` marker on the five v1alpha3 types, so controller-gen emits it and `make controller-manifests` stays the only producer of the templates | to file — prepared in the fork: branch `upstream/kagent-crds-resource-policy-keep`; giantswarm/giantswarm#37742 row 22 |
| `feat(api): a per-source credential for private git skill and plugin sources` | two of the platform's live agents take their skills from private repositories, and `main` has no credential path for artifact sources (the materialiser shells out to git with the process environment and no helper). `skills[].source.git.credentialRef` (and `plugins[].source.git.credentialRef`) names a same-namespace Secret key; the compiler turns it into one Secret-backed runtime variable per Secret key (`KAGENT_ARTIFACT_CREDENTIAL_<hash>`, hashed into provenance, the config JSON carries the variable's name only) and the materialiser offers it through a per-invocation git credential helper to the source's https host only, on challenge. Chosen over an ambient per-Harness credential in the spike giantswarm/giantswarm#37321: a missing Secret fails only the referencing templates, and the token reaches only the actors that reference it | to file — prepared in the fork: branch `upstream/artifact-source-credential-ref`; giantswarm/giantswarm#37742 row 4 |
| `fix(a2agateway): fail a task whose dispatch to the runtime fails instead of leaving it submitted` | a turn the gateway could not deliver to the AgentInstance runtime (a failing dial, a failing ingester start, or a runtime stream that errored before its first event) left its task `TASK_STATE_SUBMITTED` for good: the instance kept it as its active task, every later message was refused as a conflict and every client saw a turn that never ends. The task is now recorded `TASK_STATE_FAILED` with the cause as its status message once the runtime says it does not hold the task, the status update reaches the caller and the instance's active task is released; the reconciler fails a submitted task the runtime does not know once it is older than the dispatch grace period (one minute) | to file — prepared in the fork: branch `upstream/a2agateway-dispatch-failure`; giantswarm/giantswarm#37742 row 33 |

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
| Charts | `oci://ghcr.io/giantswarm/kagent/helm/kagent:<version>`, `oci://ghcr.io/giantswarm/kagent/helm/kagent-crds:<version>` (the chart's image defaults are stamped with the same version, `controller.agentImage.digest` and `runtimeImages.claudeHarness.digest` with the index digests of that build's golang-adk and claude-harness images, `substrateWorkerPool.workerImage` with the worker of the Makefile's `SUBSTRATE_VERSION`) |
| Dev version of a branch push | `0.11.0-dev.giantswarm.<YYYY-MM-DD>.<HH-MM-SS>.h<sha7>` — base `0.11.0` (upstream `main`'s next release), the branch lowercased to `[a-z0-9-]`, the committer date in UTC, the short commit. Consumers select the channel with a Flux `semverFilter` of `.*-dev\.giantswarm\..*` on the range `>=0.11.0-0 <0.12.0-0` |
| Release version of a tag | the tag without `v`: `X.Y.Z-gs.N` (the release scheme below; `tag.yaml` refuses any other tag shape) |
| Digests | every build's image and chart digests (multi-arch index digests, what a `Harness` pins) are a row of [`builds.md`](../../blob/ledger/builds.md) on the `ledger` branch and in the run summary of the workflow run |

Only what the platform consumes is published: the Python ADK, the Codex harness and the CLI image are not
offered on the platform and are not built here. A dev build is the test artifact of a change — a pull request's
branch or a re-pin, proven in agentlab before the tag that consumers depend on. Its consumers: the agent-platform
meta chart's dev line (`components.kagent` follows the channel; its `kagent.tag` and the two `Harness` image digests
are pinned there and re-pinned per build), agentlab (`agentlab configure --chart-branch poc/kagent-main`), and the
test cluster following the channel. A release (below) is what the meta chart's release line resolves.

Publishing does not wait for the tests. Nothing reaches the consumed branch untested — a pull request needs a
green `ci-ok` and `scan-ok` on an up-to-date branch, and the sync lands a candidate only after both passed on
its exact head (the same commits it then force-pushes) — so the push-triggered run on the consumed branch is a
repetition; a red one is a flake to re-run or an environment drift to fix, visible on the commit and in the
Actions tab, never a reason to pull a build that was green on the same tree.

### Release scheme

A release of the line is a tag `vX.Y.Z-gs.N` on the consumed branch — the scheme of the Substrate line
(giantswarm/substrate). `X.Y.Z` is the upstream version the pin anticipates: `DEV_BASE_VERSION` in `tag.yaml`,
today `0.11.0`, upstream `main`'s next release. `N` counts the line's releases of that base, from 1. The first
release is `v0.11.0-gs.1`. `tag.yaml` publishes a tag the way it publishes a dev build (`on.push.tags` `v*.*.*`
matches, the version is the tag without `v`, and the version step refuses a tag that is not
`v<DEV_BASE_VERSION>-gs.<N>`): the four images and both charts under `ghcr.io/giantswarm/kagent` at
`0.11.0-gs.1`, the digests as a row of `builds.md`. The release's row in the table below is written by hand.

Why this shape:

- **Order.** Semver sorts `0.11.0-dev.giantswarm.… < 0.11.0-gs.1 < 0.11.0`: a release outranks every dev build of
  its base and never outranks the upstream release it anticipates, so the day upstream ships everything the line
  carries, the switch to the upstream tag is a range change for the consumers, not a rename. When upstream tags
  `v0.11.0`, the next re-pin moves `DEV_BASE_VERSION` to `0.12.0` and the counter restarts at `-gs.1`.
- **No collision with the mirror.** Upstream's tags are `vX.Y.Z` (and its own pre-release suffixes); `-gs.N` is
  ours alone. The mirror copies every upstream tag the fork lacks and never touches a tag upstream does not have —
  which is also why a fork tag must never carry a name upstream will use: a `v0.11.0` here would make the mirror
  skip upstream's `v0.11.0` silently. Upstream's version resolvers in `ci.yaml` see the fork's tags too:
  `scripts/upgrade-from-version.sh` (the `adjacent` leg) is skipped on a non-release base such as `giantswarm`,
  and `scripts/prev-stable-version.sh` only looks at the release line below the one being built (`release/v0.10.x`
  today), where no `-gs.N` tag lives. The one window: after upstream opens `release/v0.11.x` and before its first
  `v0.11.*` tag, a `0.11.0-gs.N` tag would be the only `prev-stable` candidate — the re-pin that follows upstream's
  release closes it (the base moves); until then, expect that leg red and re-pin.
- **Out of the wrapper's range.** Every 3.x installation of the platform selects the 0.10 wrapper chart `kagent` at
  `oci://gsoci.azurecr.io/charts/giantswarm/kagent` with `>=0.2.0 <1.0.0` and re-resolves it on every reconcile.
  The line's chart must never be resolvable there. It is not, twice over: it lives on another OCI path
  (`ghcr.io/giantswarm/kagent/helm`), and its version is a pre-release (`-gs.N`, `-dev.…`), which Flux's semver
  (Masterminds) never matches against a range without a pre-release bound. Either alone keeps it out; both are used.
- **ghcr.io, not gsoci or the app catalog, today.** The org publishes to gsoci and the catalog from CircleCI
  (architect) only; GitHub Actions has no ACR credential, and this repository runs upstream's Actions workflows,
  not a CircleCI pipeline. Moving the line to gsoci later is a repository-URL change for the consumers
  (`components.kagent`/`components.kagent-crds` `repository`, the `Harness` image references) plus a push
  credential in the publish job — the version scheme does not change.

A tag is cut only after the build of the same commit — its dev build — has passed the platform's proofs in
agentlab on the meta chart's dev channel; the tag re-publishes the same tree under the release version. Cut it
deliberately (no tag ruleset gates it; anyone with write access can push a tag):

```bash
git tag v0.11.0-gs.1 <sha> && git push origin v0.11.0-gs.1
# or, without a checkout:
gh api -X POST repos/giantswarm/kagent-upstream/git/refs -f ref=refs/tags/v0.11.0-gs.1 -f sha=<sha>
```

Then add the row below from the run summary (or `builds.md`).

### Releases

| Release | Tag | Commit | Upstream pin | Images (index digests: controller, ui, golang-adk, claude-harness) | Charts (kagent, kagent-crds) |
|---|---|---|---|---|---|
| `0.11.0-gs.1` | `v0.11.0-gs.1` (2026-09-10T23:36Z, after the agentlab proof of the dev build `0.11.0-dev.giantswarm.2026-09-10.22-06-46.h0ac5240`: platform, agents and toolsets proofs green) | `0ac5240` | `4a91c27` (2026-09-10) | `sha256:04106af50ee8e68e0388006981e25bd61b5220f50ece8fe7920269aea3b8d49c`, `sha256:3275c7ab8f09291f4806ed49e406d98c1dbec51e5d05415ed07ade2799423f81`, `sha256:969af5f733c8e2bd7756f40766352f5af744964e969a031ba146198d8becd546`, `sha256:c8a9c7c3d5dd7ecc3953fff2a12b6b4a44b46eb6a66687a5604f6851fe453ca3` | `sha256:3c9ee22cb60c493b7e99bbc27d260cb5aa5e48d0996db1bbd28abae3b17a8a9e`, `sha256:7c62f76e5693bad9b603f4e557b82da9b54060e3d1f811133905b4d6cca76d70` ([run 34542852237](https://github.com/giantswarm/kagent-upstream/actions/runs/34542852237)) |
| `0.11.0-gs.2` | `v0.11.0-gs.2` (2026-09-11T04:55Z, after the agentlab criterion-5 proof of the dev build `0.11.0-dev.giantswarm.2026-09-11.00-32-34.h3bdff38`: reached directly through a port-forward to `kagent-controller` — bypassing agentgateway — `SystemService/GetCurrentUser` returned the admin bearer's `email` while a forged `x-user-id` was ignored, a bearer-less call was refused (`Unauthenticated`), `/health` stayed `200`, and the five CRDs carried `helm.sh/resource-policy: keep`) | `3bdff381` | `4a91c27` (2026-09-10) | `sha256:9f0acec1ce78e7c9b261c199dd73be24ec404e389138a4ed909bf2e5527b8729`, `sha256:de6d8a593a49e013799a38a84d43586f2ebb84905d84b826783a343dddf88e82`, `sha256:7db42765cc401f4e356f876cf76a25de39c5109e56fabc9fe45ec0bf7e2d3137`, `sha256:82fce79679ac39a386a393aa0fbba7a2f55652bd1bc184adc5d5eae72c040c82` | `sha256:80026ab9dfbe9d82cbd2f856890cf389c58dc92c35c141af25e0a5d3e77b0c87`, `sha256:00b47fba8ae09e4e62cc5c4d6c6a40c2bb5cc596ab005e6b3d929fd6a4ec1b8c` ([run 34563969164](https://github.com/giantswarm/kagent-upstream/actions/runs/34563969164)) |
| `0.11.0-gs.3` | `v0.11.0-gs.3` (2026-09-11T07:26Z, after the agentlab proof of the dev build `0.11.0-dev.giantswarm.2026-09-11.06-51-42.hed07617` — the second automated re-pin's head, upstream `6a6b54ff`: `platform-test`, `agents-test`, `toolsets-test --skip-portal` and `skills-test` green on an isolated lab running the build as charts, images and both platform Harness digests) | `ed07617b` | `6a6b54ff` (2026-09-10) | `sha256:eeac144a79ad1c183208eeeb699a362ccc7dffd70f1032df8f5b57856dba85ac`, `sha256:77154fd68b88df89037df0c83dc26a98f1f10db5024ea8d5ca8d29cfa9e55133`, `sha256:a2d23f5eb9c01e1903459a6e742f7d4aaa5e950d7e9aa6f07f8982761be0163a`, `sha256:d8df16dc5246130ccfb1cd0ef5b6665f354d02812d2266176a3d230096556617` | `sha256:50451bb05a4706ea90503c28c574e82f64fefca5cb45ec749bf0f3e639fbd0b3`, `sha256:d7150eb061713bd42ddfd7506ebd9db7fd39c2e80b68d2bdd41100536cbb6a72` ([run 34574409389](https://github.com/giantswarm/kagent-upstream/actions/runs/34574409389)) |
| `0.11.0-gs.4` | `v0.11.0-gs.4` (2026-09-12T10:22Z, on the rebase-merged giantswarm/kagent-upstream#15 — the A2A gateway fails a task whose dispatch to the runtime fails — cut for the agentlab 4.x audit's re-pin of the meta chart, giantswarm/giantswarm#37763; the pin and the other carried patches are those of `gs.3`) | `ca399a3a` | `6a6b54ff` (2026-09-10) | `sha256:fc3eb5169a66276c467f23bf28bf932e6c7f8f92c7b56548c1f7fb38cc5868a1`, `sha256:93627659768dc3d447672807074e0d53993efb992c3cb58c85d05fb9b6ad57e3`, `sha256:6e510b241e2d000b7d10879c9fde3dd34a37879854278f0a5d386426049a88bf`, `sha256:475741e15dbe4c303a44bb80d672c05511dc3eb4118acf32b0f9ab3124a235dc` | `sha256:f5f037613d70fb982c23b4342f2d3760d828ebcc1b81e6d7a6c09944e142ca23`, `sha256:74a093857d183d5ff67fe81f3787df0699929643b7a9fbf291ae3955c548e183` ([run 34688265253](https://github.com/giantswarm/kagent-upstream/actions/runs/34688265253)) |

## CI and security

`ci.yaml` (upstream's suite: Go unit tests and lint, Helm unit tests, protobuf contract check, manifests check,
UI typecheck/lint/unit/browser tests, Python tests and lint, multi-arch image builds, the e2e against kind +
Substrate, the upgrade legs) and `image-scan.yaml` (Trivy on every published image at CRITICAL/HIGH,
`govulncheck` over the Go module graph) run on every push to `giantswarm` and `sync/**`, on every pull
request to `giantswarm`, and — the scan — weekly. Fork differences from upstream's files: the triggers,
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
4. green: force-pushes the candidate onto `giantswarm` (`--force-with-lease` on the head it started
   from), deletes the candidate, appends the row to `re-pins.md`; `tag.yaml` publishes the dev build and
   appends its row to `builds.md`;
5. a conflict or a red check: nothing is pushed to the consumed branch; a pull request against
   `giantswarm` names the conflicting carried commit (with the files) or the failed checks, and the
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
# on a conflict: git rebase --onto upstream/main "$(git merge-base origin/giantswarm upstream/main)" ; fix ; git rebase --continue
git push origin "$CANDIDATE"                      # ci.yaml + image-scan.yaml run on sync/** ; wait for ci-ok and scan-ok
git push --force-with-lease=refs/heads/giantswarm origin "$CANDIDATE":giantswarm
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
  (`fix/…`, `feat/…`) and open the pull request here against `giantswarm` with the upstream link; the
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
