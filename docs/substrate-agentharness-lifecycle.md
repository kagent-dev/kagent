# Substrate AgentHarness Lifecycle

This branch should use a single ownership model for `runtime: substrate` harnesses.

## Ownership

- Platform/Helm owns `WorkerPool` capacity.
- kagent owns the generated per-harness `ActorTemplate`.
- kagent owns the per-harness actor lifecycle through `ate-api`.
- Substrate owns the `WorkerPool` deployment and the `ActorTemplate` golden snapshot process.

kagent should not create or delete `WorkerPool` resources from the `AgentHarness` reconciler. A chart may optionally install a default `WorkerPool`, and the controller may use that default when `spec.substrate.workerPoolRef` is unset.

## Spec Shape

`AgentHarness.spec.substrate` should contain only harness-level inputs:

- `workerPoolRef`, optional; falls back to the configured controller default.
- `snapshotsConfig`, optional; defaults to `gs://ate-snapshots/<namespace>/<name>`.
- `workloadImage`, optional.
- exactly one of `gatewayToken` or `gatewayTokenSecretRef`.

There is no `actorTemplateRef`. kagent always generates the `ActorTemplate`, so adopting an external template is not part of the workflow.

## Status

Use top-level Kubernetes conditions for progress:

- `Accepted`
- `ActorTemplateReady`
- `ActorReady`
- `Ready`

`Ready` is the aggregate condition. Specific blockers should be reflected in `reason` and `message`.

Do not store ownership booleans or cleanup markers in annotations or status. Ownership is deterministic:

- `WorkerPool` is external.
- generated `ActorTemplate` is owned by the `AgentHarness` through an owner reference.

## Reconcile

The substrate reconcile path should:

1. Resolve `workerPoolRef` from spec or controller default.
2. Verify the `WorkerPool` exists.
3. Create or update the generated `ActorTemplate` with an owner reference to the `AgentHarness`.
4. Wait for `ActorTemplate.status.phase == Ready`.
5. Create or resume the actor through `ate-api`.
6. Mark `ActorReady` and aggregate `Ready`.

## Delete

The finalizer should:

1. Delete the harness actor recorded in `status.backendRef.id`.
2. Read the generated `ActorTemplate` and delete `status.goldenActorID`, if present.
3. Remove the finalizer.

Kubernetes garbage collection deletes the generated `ActorTemplate` through the owner reference. kagent does not delete `WorkerPool`.
