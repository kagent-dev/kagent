# Standalone sandbox implementation

This sequence implements the [approved API and persistence design](https://gist.github.com/EItanya/8867e70fbde9618e5d7c5432491d2e92).
AgentInstances keep their existing A2A lifecycle. Agents create independent scratch
sandboxes through MCP; guest execution belongs to those sandboxes.

Harness and SandboxTemplate are independent configuration resources. Harness owns
the agent image, startup, adapter configuration, environment, Substrate policy,
and AgentTemplate admission. SandboxTemplate owns the standalone sandbox image,
environment, and Substrate policy. They may share Go field types and validation
where semantics match; Harness does not reference or consume SandboxTemplate.

1. **Guest runtime — merged in [#2911](https://github.com/kagent-dev/kagent/pull/2911).**
   Kagent owns the entrypoint and imports the pinned env guest services. Server
   tests run in-process in the existing Go test job.
2. **SandboxTemplate catalog — this change.** Add the namespaced v1alpha3 CRD,
   generated Kubernetes clients, Helm packaging/RBAC, and authenticated gRPC
   list/create/delete operations following HarnessService. Keep the CRD as the
   schema source of truth through StructuredObject. Test validation against a real
   Kubernetes API server and exercise the public RPCs and collection scopes.
3. **Shared runtime persistence.** Add runtime revision/instance bases with typed
   agent and sandbox extensions in a new migration. Preserve agent lifecycle,
   retry identity, tombstones, and checkpoint/revision retention. Enforce matching
   kinds and exactly one extension transactionally; test migration, admission,
   retries, deletion, and garbage collection against PostgreSQL. Keep Harness and
   AgentTemplate provenance on agent revisions and SandboxTemplate provenance on
   sandbox revisions; shared persistence does not couple the configuration APIs.
4. **Standalone preparation and lifecycle.** Prepare a pinned guest runtime from
   SandboxTemplate, then expose separately authorized Sandbox creation, listing,
   inspection, and deletion with bounded expiration. Establish resource/egress
   policy and uncertain-backend recovery before enabling the new runtime path.
   Track template UID and resolved inputs in each immutable sandbox revision.
   Template changes prepare new sandbox revisions; existing instances and retries
   retain their pins. Template deletion must preserve retained runtime inputs.
5. **Guest access and MCP.** Add authenticated process/file APIs scoped to a
   Sandbox identity, with lifecycle admission and explicit ambiguous-start
   handling. Expose scratch tools through the existing MCP service with verified
   caller identity and delegation. Validate against live Substrate Actors.

## Catalog boundary

The initial resource reuses the existing HarnessEnvVar and HarnessSubstratePolicy
Go types and their validation. Its workload contains only a digest-pinned image;
it has no startup command, adapter, or guest toggle. Creating configuration
allocates no compute. There is no preparation status until a consumer can
establish it.

Each resource owns its configuration independently, even where field shapes and
values coincide. There is no configuration inheritance, Harness cutover, or agent
runtime package composition in this plan. Renaming shared Go types can be a
separate mechanical cleanup if needed; it does not change either resource's API.

An illustrative template (replace the image digest before preparing a runtime):

```yaml
apiVersion: kagent.dev/v1alpha3
kind: SandboxTemplate
metadata:
  namespace: team-a
  name: scratch
spec:
  workload:
    image: registry.example.com/tools@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  env:
    - name: LANG
      value: C.UTF-8
  substrate:
    workerPoolRef:
      name: default
    snapshotPolicy:
      location: s3://example/snapshots/
```

The catalog uses the same scoped `namespace`/`name` authorization as Harnesses.
Create/delete require resource authorization; list filters before sorting and
response construction. References to worker pools or secrets grant no access to
those resources. Secret values are never resolved by the catalog.

Consumers must preserve the existing rejection of arbitrary credential environment
injection and reserved runtime-variable collisions. CPU/memory policy, generic
egress, standalone lifetime defaults/limits, and agent delegation still require
implementation before a standalone Sandbox service is usable. Template configuration
alone does not make those capabilities available.
