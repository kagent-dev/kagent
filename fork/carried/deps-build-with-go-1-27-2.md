---
id: deps-build-with-go-1-27-2
fork: kagent
theme: version-pins
category: pin
status: carried
commits:
  - 'fix(deps): build with Go 1.27.2 and golang.org/x/net v0.60.0'
upstream:
  outcome: drops-at-repin
  refs: [kagent-dev/kagent#3229]
  exit: drops when upstream builds with the same toolchain
depends_on: []
verified: 2026-10-19
---
# fix(deps): build with Go 1.27.2 and golang.org/x/net v0.60.0

## Change

`fix(deps): build with Go 1.27.2 and golang.org/x/net v0.60.0`

## Reason

the October Go security release (GO-2026-6599 to GO-2026-6617: net/http HTTP/1 and HTTP/2, html/template, crypto/tls, net/textproto) is reached from kagent code and fails `govulncheck` on every pull request; fixed in Go 1.27.2 and, for the HPACK encoder race, x/net v0.60.0. The `go` directive drives the CI toolchain (`go-version-file`) and the image builds (`TOOLS_GO_VERSION`)

## History

to file (upstream main is on Go 1.27.0 and x/net v0.59.0); dropped at the re-pin onto an upstream that has both
