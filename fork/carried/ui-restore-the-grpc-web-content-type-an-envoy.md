---
id: ui-restore-the-grpc-web-content-type-an-envoy
fork: kagent
theme: chart-knobs
category: bugfix
status: carried
commits:
  - 'fix(ui): restore the gRPC-Web content type an Envoy grpc_web filter rewrote'
upstream:
  outcome: to-file
  refs: []
  exit: file
depends_on: []
verified: 2026-10-19
---
# fix(ui): restore the gRPC-Web content type an Envoy grpc_web filter rewrote

## Change

`fix(ui): restore the gRPC-Web content type an Envoy grpc_web filter rewrote`

## Reason

the platform's ingress is Envoy Gateway, which runs Envoy's `grpc_web` filter on every route of a listener that also carries a GRPCRoute; it rewrites the UI's gRPC-Web calls into `application/grpc`, which reach the controller over HTTP/1.1 as neither gRPC nor gRPC-Web, so the controller's plain HTTP handler answers an empty 200 and a signed-in Dashboard reads no agents, model configurations, MCP servers or tools (giantswarm/agent-platform#991); the UI's nginx now forwards such a call to `/api/` with the matching gRPC-Web content type

## History

to file — prepared in the fork: branch `upstream/ui-nginx-grpc-web-behind-envoy` (mirror `main@92dd6caa` + the patch, DCO-signed, no fork references); no upstream issue or pull request on the grpc_web content type found (search `repo:kagent-dev/kagent grpc-web envoy`)
