# Standalone sandbox guest

This image builds kagent's [guest command](cmd/main.go), using the public
`agent-substrate/env/guest` library at the version pinned in `go/go.mod`. Kagent
owns the entrypoint, flags, logging, HTTP listener, readiness, and shutdown.
Upstream provides the gRPC process and filesystem services. No AX code is used.

The image provides the runtime for [standalone sandboxes](../../../docs/architecture/sandboxes.md). The control plane owns
sandbox creation, authorization, expiration, persistence, and MCP access.
Sessions do not run this guest.

## Runtime contract

- The static binary is `/usr/local/bin/kagent-sandbox-guest`.
- The daemon runs as UID/GID `65532:65532` and starts no agent runtime.
- Port `80` serves both HTTP `GET /readyz` and plaintext HTTP/2 gRPC.
- The upstream `ProcessService` and `FileSystemService` protocols are unchanged.
- The default process working directory and file API root are `/data/workspace`.
- Process output is spooled under `/data/guest-logs`.
- Both directories must be writable by the runtime user when mounting `/data`.
- Flags `--listen`, `--workspace`, and `--log-dir` override these defaults.
- JSON logs go to stderr; `LOG_LEVEL` selects debug, info (default), warn, or error.
- SIGINT/SIGTERM stop serving and disconnect active RPCs, including output
  observers, before cleaning up the guest services and exiting.

The guest is a private runtime endpoint, with no caller authentication or resource
ownership enforcement. The kagent sandbox service authorizes and admits
calls before routing them to it. The workspace path is a convenience boundary;
processes can access files allowed by their OS permissions, and filesystem path
checks do not provide isolation from symlinks. The Actor supplies isolation.

The image includes Bash, Git, and CA certificates. Other workload images can copy
the static binary and supply their own tools and writable directories:

```dockerfile
ARG GUEST_IMAGE
FROM ${GUEST_IMAGE} AS guest
FROM your-workload-image
COPY --from=guest /usr/local/bin/kagent-sandbox-guest /usr/local/bin/kagent-sandbox-guest
# Prepare /data/workspace and /data/guest-logs for this image's runtime user.
ENTRYPOINT ["/usr/local/bin/kagent-sandbox-guest"]
CMD ["--listen=:80", "--workspace=/data/workspace", "--log-dir=/data/guest-logs"]
```

Use an immutable guest image digest for reproducible packaging. SandboxTemplate
preparation mounts that image and starts its binary in the selected tools image;
copying it into each workload image is unnecessary. The launcher creates the
workspace before readiness, including when `/data` masks the image's directories.

## Build and verify

From the repository root, build into the local Docker daemon without publishing:

```sh
make build-sandbox-guest \
  SANDBOX_GUEST_IMG=kagent-sandbox-guest:dev \
  DOCKER_BUILD_ARGS='--load --platform linux/amd64'
```

Use `linux/arm64` on an ARM host. Run the server tests from `go/`:

```sh
go test -race ./sandbox/guest/... -count=1 -v
```

The tests run the server in-process on a local listener and require no image or
Docker daemon. They cover readiness, process service registration, file transfer
in the configured workspace, startup failures, and cancellation-driven shutdown.
The existing Go unit-test job runs them. The sandbox E2E suite also exercises the
image through the Substrate router, preparation snapshots, and suspend/resume.

## Upstream semantics

Process identities and status are in memory. Restarting the guest loses the
registry even if workspace files survive. Filesystem persistence does not resume
processes or provide durable process results. `StartProcess` has no idempotency
key; never blindly retry after an ambiguous response.

The pinned service defaults to ten concurrent processes, a one-hour process
timeout, and 10 MiB of output per stream. Output truncation, completed-process
retention, partial file writes, and termination follow upstream behavior. In
particular, writes replace the destination directly; an interrupted transfer can
leave a partial file. Shutdown is not a checkpoint or a durable-completion
protocol. These limits must remain explicit when adding the public sandbox API.
