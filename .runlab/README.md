# .runlab

Labs that run kagent in a microVM on your own machine, written for [runlabs.io](https://runlabs.io)'s `trylab`.

| lab | what it is for |
|---|---|
| [`pr-review`](pr-review) | try a pull request: check it out, build it, install it in a kind cluster, read its diff, open the kagent UI |

## Try a pull request

From anywhere inside your kagent checkout:

```
trylab pr 3564
```

`trylab pr` reads the pull request with your own `gh`, opens `pr-review`, and puts the pull request's title, author and changed files above the lab's steps. The lab then has two panes: the build runs in the bottom one while you read `gh pr diff` in the top one, and a link at the end opens the kagent UI on that pull request.

The microVM is discarded when you finish. The pull request's code runs inside it, not on your machine, and sees only what `pr-review/config.yaml` passes in (below).

## What you need

- A Linux x86_64 machine with KVM. The [author pack](https://runlabs.io/author-pack) installs the rest: unpack it, run `./prereqs` once, and put its directory on your `PATH`.
- `gh`, logged in on your machine: `trylab pr` uses it to read the pull request.
- About 16 GiB of memory free for the VM, and about 40 GB of disk for this lab (a 23 GB disk and a 17 GB memory snapshot).
- Two variables in your environment when you start:
  - `OPENAI_API_KEY`: the model provider for the agents (the lab sets `MODEL_PROVIDER=openAI`; change that line in `pr-review/config.yaml` for another provider). The key is passed into the VM.
  - `GH_TOKEN`: the VM's own `gh` checks out and diffs the pull request, and needs a token even for a public repository. Use a **fine-grained token with no permissions beyond reading public repositories**, not your login's token, because the VM runs the pull request's code.

## The first run

The first run builds the lab's disk and snapshot: it installs the toolchain, clones `main`, creates the kind cluster and its registry, and builds kagent's images once so that the caches are warm. That takes a long time and a lot of disk. After that a `trylab pr` starts in seconds, and the per-pull-request work is the checkout, `make build` and `make kagent-cli-install`.

Kagent and Substrate are **not** in the snapshot. Substrate's pod certificates last 24 hours and are not renewed after a snapshot resumes, so the lab installs them for each pull request.

### Refreshing the snapshot

The snapshot holds `main` as it was on the day it was built. A pull request that bumps a toolchain or changes the build may build slowly or fail on an old snapshot. To rebuild, delete the lab's disk and snapshot from your pack directory (`filesystems/kagent-pr-review.ext4`, its `.stamp`, and `snapshots/kagent-pr-review`), and the next `trylab pr` builds again. `trylab` also rebuilds on its own when `pr-review/setup.sh` or the files in `pr-review/data/` change.

## Learn more

To learn how labs are written and how they work, start at [runlabs.io/learn](https://runlabs.io/learn). `trylab --help` lists the commands and the variables `trylab` reads.
