# Review a kagent pull request

This lab holds a kind cluster, the project's toolchain and a clone of `main`, with the build caches warm.
Kagent itself is not installed yet: this builds the pull request and installs it.

## Start the build

The pull request is already checked out in the lab: trylab gave it the commits of your own checkout, so it is
the branch you are on. Build its images and install it.

```shell panel="build"
make build &&
  make kagent-cli-install
```

The build takes a while. Read about the pull request below while it runs.

## Read the changes

Show what the pull request changes, against the commit it branched from:

```shell panel="view"
git diff <merge-base>...HEAD
```

## Open kagent

When the install has finished, the link opens the kagent UI on this pull request:

```service kagent
```
