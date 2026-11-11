# Review a kagent pull request

This lab holds a kind cluster, the project's toolchain and a clone of `main`, with the build caches warm.
Kagent itself is not installed yet: this builds the pull request and installs it.

## Start the build

Check out the pull request, build its images and install it.

```shell panel="build"
gh pr checkout <pr> &&
  make build &&
  make kagent-cli-install
```

The build takes a while. Read about the pull request below while it runs.

## Read the changes

Once the checkout in the build pane has finished, show what the pull request changes (until then there is nothing to compare):

```shell panel="view"
gh pr diff <pr>
```

## Open kagent

When the install has finished, the link opens the kagent UI on this pull request:

```service kagent
```
