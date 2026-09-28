# Issue #2764 explained from zero: why `Harness.status` is empty and what must be built

This document explains [kagent issue #2764](https://github.com/kagent-dev/kagent/issues/2764) from first principles. It assumes you know basic programming but do not yet know Kubernetes controllers or kagent's current architecture.

## 0. The big picture

kagent lets a user describe an AI agent using Kubernetes objects. **Kubernetes** is a system that stores structured descriptions of software and lets controllers continuously act on them. A **controller** is a long-running program that watches those descriptions and adjusts the system. Two objects matter here:

- A `Harness` describes **how agents run**. For example, it selects the kagent, Codex, Claude, or bring-your-own runtime, names a container image, names a Substrate worker pool, and configures snapshots.
- An `AgentTemplate` describes **what an agent does**. For example, it supplies a model, prompt, tools, skills, and plugins.

The kagent controller combines one `Harness` with one compatible `AgentTemplate`, compiles them, and prepares something runnable on **Substrate**, the compute backend that starts and manages agent processes. Here **compile** means transforming user-facing configuration into explicit, immutable runtime inputs.

The bug is that Kubernetes stores the user's requested `Harness` configuration, but the kagent controller never writes back what it observed about that `Harness`. Consequently:

1. `kubectl get harness` displays an empty `READY` column. `kubectl` is Kubernetes's command-line program.
2. `kubectl get harness <name> -o yaml` has no useful `status` section.
3. kagent's gRPC API reports the Harness as not ready even when it is usable. An **API**, or **Application Programming Interface**, is a defined way for programs to exchange requests and responses. **gRPC** is the remote-call protocol used by this kagent API.
4. The declared `status.capabilities` record cannot be consumed because nobody creates it.

The API contract exists. The generated **Custom Resource Definition**—the schema that teaches Kubernetes about the `Harness` kind—exists. Kubernetes permits status updates. **Helm**, Kubernetes's package manager, already installs Role-Based Access Control permissions allowing the controller to update `harnesses/status`. The missing piece is controller logic.

This is a medium controller change, not a one-line fix. Once the semantics are agreed, the mechanical status pipeline and focused tests are approximately 4–6 hours of work. Defining and proving a 13-field capability record for 4 adapters can expand the task to 1–2 working days because it creates as many as 52 behavioral claims.

A simplified flow should look like this:

```text
User writes Harness.spec
        |
        v
Kubernetes stores generation 3
        |
        v
kagent controller observes the Harness
        |
        +--> checks Harness-owned references and runtime type
        |
        +--> derives capabilities from an approved catalog
        |
        v
Controller writes Harness.status
        |
        v
kubectl shows READY=True and observedGeneration=3
```

Today the flow stops immediately after the controller observes the `Harness`.

## 1. Kubernetes objects: desired state and observed state

Kubernetes stores data as objects. An object normally has two conceptually different sections:

- `spec` is the **desired state**: what the user wants.
- `status` is the **observed state**: what a controller has actually seen and concluded.

A **controller** is a long-running program that watches objects and continually tries to make observed reality match desired state. The repeated observe-and-adjust process is called **reconciliation**.

Consider this numerical example. A user creates a `Harness` whose Kubernetes metadata has `generation: 1`. The user later changes the image once, producing `generation: 2`, and changes the worker pool once, producing `generation: 3`. If the controller has processed the latest version, it writes:

```yaml
status:
  observedGeneration: 3
```

The number matters because `generation: 3` with `status.observedGeneration: 2` means the displayed status describes the previous configuration, not the current one.

### 1.1 A Custom Resource Definition

A **Custom Resource Definition**, abbreviated **CRD**, teaches Kubernetes a new object kind. Kubernetes knows built-in kinds such as `Pod`; kagent installs CRDs for custom kinds such as `Harness` and `AgentTemplate`.

The source type for `Harness` is [`go/api/v1alpha3/harness_types.go`](go/api/v1alpha3/harness_types.go). It declares both fields:

```go
Spec   HarnessSpec   `json:"spec"`
Status HarnessStatus `json:"status,omitempty"`
```

The generated CRD at [`go/api/config/crd/bases/kagent.dev_harnesses.yaml`](go/api/config/crd/bases/kagent.dev_harnesses.yaml) contains `subresources: status: {}`. A **status subresource** is a Kubernetes API endpoint dedicated to status writes. It lets the controller update `status` without pretending to be the user who owns `spec`.

### 1.2 Conditions

A Kubernetes **condition** is a structured statement about one aspect of an object's state. It contains at least:

- `type`: the question, such as `Ready`.
- `status`: `True`, `False`, or `Unknown`.
- `reason`: a short machine-readable explanation, such as `ReferencesResolved`.
- `message`: a human-readable explanation.
- `observedGeneration`: the object generation evaluated by this condition.
- `lastTransitionTime`: when the condition meaningfully changed.

For example, suppose generation 3 references worker pool `gpu-pool`, but that pool does not exist. A possible condition is:

```yaml
- type: Ready
  status: "False"
  reason: WorkerPoolNotFound
  message: 'WorkerPool "gpu-pool" was not found in namespace "team-a"'
  observedGeneration: 3
  lastTransitionTime: "2026-09-16T10:00:00Z"
```

If `gpu-pool` is created five minutes later, reconciliation runs again and can change the condition to `Ready=True`. Because the status changed from false to true, `lastTransitionTime` should change to approximately `10:05`. If reconciliation runs again at `10:06` and the condition is still identical, `lastTransitionTime` must remain `10:05`; otherwise it would measure controller activity rather than an actual state transition.

### 1.3 The `READY` column is not automatic

The `Harness` CRD declares this printer column:

```go
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
```

The marker's `JSONPath` is a small query that selects a field from the object's JSON representation. **JSON**, or **JavaScript Object Notation**, is the structured data format used by the Kubernetes API. The marker tells `kubectl` where to read the display value. It does not calculate the value.

If 4 Harnesses have no `Ready` condition, all 4 show a blank column. Kubernetes is not malfunctioning; it was told to print a field that nobody writes.

## 2. What a kagent Harness represents

A `Harness` is a reusable runtime policy. One Harness can admit many `AgentTemplate` objects.

For a concrete example, imagine namespace `team-a` contains:

- 1 Harness named `codex`.
- 3 AgentTemplates named `reviewer`, `researcher`, and `operator`.
- 1 Substrate WorkerPool named `default`.

The Harness can specify one digest-pinned OCI image, such as `example.com/codex@sha256:<64 hexadecimal characters>`, and all 3 admitted templates can reuse that runtime policy. **OCI**, or **Open Container Initiative**, defines the standard container-image format. A **container image** is a packaged filesystem plus metadata used to start a process. A **digest-pinned image** identifies exact image bytes; unlike a mutable tag such as `latest`, the digest cannot silently point to different bytes tomorrow.

The `HarnessSpec` contains four important categories:

1. Exactly one runtime adapter: `kagent`, `codex`, `claude`, or `byo`. A **runtime adapter** translates common kagent configuration into the format understood by one agent runtime. `byo` means **bring your own** runtime image.
2. Workload configuration: image, command, and arguments.
3. Substrate configuration: worker-pool reference and snapshot location. A **WorkerPool** names a group of compute capacity eligible to run actors. An **actor** is one running agent process managed by Substrate. A **snapshot** is saved actor state that can be restored later.
4. An admission selector that chooses which `AgentTemplate` labels this Harness accepts.

Kubernetes CRD validation already rejects structurally invalid input. For example, selecting both `codex` and `claude` gives a count of 2 when the rule requires exactly 1. However, structural validation cannot prove that a referenced WorkerPool or Secret exists. That needs controller observation.

## 3. What `HarnessStatus` promises

The public type currently declares three status fields:

```go
type HarnessStatus struct {
    ObservedGeneration int64                `json:"observedGeneration,omitempty"`
    Capabilities       *HarnessCapabilities `json:"capabilities,omitempty"`
    Conditions         []metav1.Condition   `json:"conditions,omitempty"`
}
```

`ObservedGeneration` answers, “Which Harness revision did the controller evaluate?”

`Conditions` currently has one declared condition type, `Ready`. It answers, “Does the controller consider this Harness usable under the agreed readiness definition?”

`Capabilities` describes behavior supported by the selected runtime adapter. A **capability** is a behavior that consumers may safely expect, rather than a user preference. The record contains values such as:

- whether native agent tools are supported;
- the maximum native-agent nesting depth;
- whether dedicated agent tools and MCP injection are supported; **MCP**, or **Model Context Protocol**, is a protocol through which an agent discovers and calls external tools;
- whether streaming, interruption, user input, and approvals are supported;
- accepted input and output modalities, such as `text`;
- whether resume and checkpoint operations are supported.

Suppose catalog version `v1` states that one adapter supports streaming and user input but not checkpoints. A consumer should be able to read 3 concrete facts from one record: `streaming=true`, `inputRequired=true`, and `checkpoint=false`.

The roadmap in [`docs/plans/api-v2-execution-plan.md`](docs/plans/api-v2-execution-plan.md) explicitly says, “Harness status publishes one controller-derived capability record.” Therefore, `capabilities` is intended to be real controller output, not a user-supplied field that should simply be deleted.

## 4. The current broken flow, one file at a time

### 4.1 The type declares status

[`go/api/v1alpha3/harness_types.go`](go/api/v1alpha3/harness_types.go) defines `HarnessStatus`, `HarnessCapabilities`, and `HarnessConditionTypeReady`. These are written in **Go**, the programming language used for kagent's controller.

This establishes the contract but performs no work. Defining a Go struct is like printing a blank form: it determines which boxes exist, but it does not fill them.

### 4.2 Kubernetes and Helm are already prepared

The generated Harness CRD enables the status subresource. Helm's [`helm/kagent/templates/rbac/getter-role.yaml`](helm/kagent/templates/rbac/getter-role.yaml) grants `get`, `patch`, and `update` on `harnesses/status`.

**RBAC** means **Role-Based Access Control**. It determines which identity may perform which operation. Here the controller already has 3 allowed status operations, so the bug is not a permission failure.

### 4.3 The gRPC service expects the controller to fill status

[`go/core/internal/grpcserver/harness.go`](go/core/internal/grpcserver/harness.go) intentionally removes user-provided status during creation:

```go
incoming.Status = v1alpha3.HarnessStatus{}
```

That is correct ownership: users author `spec`; the controller owns `status`.

The same service calculates its public `ready` Boolean from the condition:

```go
Ready: meta.IsStatusConditionTrue(
    object.Status.Conditions,
    v1alpha3.HarnessConditionTypeReady,
),
```

If there is no condition, the result is `false`. Therefore, with 10 healthy Harnesses and zero status writers, the API reports all 10 as not ready.

The existing gRPC test even says a newly created Harness should be false “before the controller observes it.” That wording assumes observation will eventually produce status, but no current controller path does so.

### 4.4 The controller watches Harnesses but only uses them as inputs

[`go/core/internal/controller/collections.go`](go/core/internal/controller/collections.go) creates a Kubernetes Runtime Toolkit collection named `Harnesses`.

The **Kubernetes Runtime Toolkit**, abbreviated **KRT**, represents watched Kubernetes objects and values derived from them as collections. When an input changes, KRT recalculates dependent collections.

The controller currently derives these important outputs:

- `ModelConfigStatuses` from `ModelConfig` objects and their references.
- `AgentTemplateStatuses` from `AgentTemplate`/Harness pair reconciliation.

It does **not** derive `HarnessStatuses`. The `Collections` struct has no such field, and `NewCollections` does not create one.

### 4.5 The side-effect writer has no Harness queue

[`go/core/internal/controller/reconciler.go`](go/core/internal/controller/reconciler.go) is the side-effect boundary. A **side effect** is an externally visible change, such as writing to Kubernetes.

For AgentTemplate status, the code performs 4 steps:

1. KRT derives the desired status.
2. A registered handler notices that desired and stored statuses differ.
3. A work queue receives the stable key `team-a/reviewer`.
4. `reconcileAgentTemplateStatus` calls Kubernetes `UpdateStatus`.

ModelConfig status uses the same pattern.

Harness status has none of those 4 steps. No desired status exists, no handler is registered, no queue runs, and no `UpdateStatus` call is made.

That complete absence is the root cause. There is no mysterious race or serialization bug.

## 5. What `Ready` should and should not mean

This is the first real design decision.

A tempting but incorrect interpretation is: “`Ready=True` means every future agent using this Harness will run successfully.” The controller cannot know that from the Harness alone. One AgentTemplate might name a valid model while another names a missing model. The Harness is shared, so template-specific success belongs in `AgentTemplate.status`, not `Harness.status`.

A bounded interpretation is:

> `Harness Ready=True` means the controller recognizes this Harness's runtime adapter and all Harness-owned references required for preparation currently resolve.

“Harness-owned” means fields inside `Harness.spec`, not fields inside an AgentTemplate. Depending on the agreed scope, checks may include:

- the named WorkerPool exists in the same namespace;
- every `spec.env[].credentialRef` Secret and key exists;
- a kagent memory `modelConfigRef` exists and is usable;
- the selected adapter is registered by the controller.

For example, if a Harness owns 3 references—1 WorkerPool, 1 Secret, and 1 memory ModelConfig—and 2 resolve while the Secret is missing, readiness is false. Once all 3 resolve, readiness can become true.

What it should not claim:

- It should not prove that an arbitrary AgentTemplate compiles; pair status owns that fact.
- It should not pull the image to prove the bytes execute; preparation of a concrete pair and its golden snapshot owns that fact.
- It should not report `Ready=True` merely because the CRD admitted structurally valid YAML; that would make the condition almost useless.

The issue does not fully specify this boundary. A maintainer should confirm it before implementation.

There is also a naming mismatch. The issue text suggests adding at least an `Accepted` condition, but the current API declares only `HarnessConditionTypeReady`, the `kubectl` column reads `Ready`, and the gRPC response reads `Ready`. Adding only `Accepted` would leave both user-visible readiness paths blank or false. The minimal implementation should therefore populate `Ready`; adding a separate `Accepted` condition requires an explicit API-semantics decision.

## 6. The capability catalog is the second design decision

The code defines the shape of `HarnessCapabilities` but contains no values and no function that maps `kagent`, `codex`, `claude`, or `byo` to a capability record.

The `Version` field is evidence that the intended design is a versioned catalog. For example, a hypothetical `v1` catalog might map 4 runtime types to 4 fixed records. If a later release proves checkpoint support for Codex, the controller can publish catalog version `v2` instead of silently changing the meaning of `v1`.

However, values must not be guessed from the presence of code. The roadmap says Codex and Claude should publish only capabilities proven by conformance tests. **Conformance tests** check that different runtime adapters actually obey the same behavioral contract. A function named `Resume` existing somewhere is weaker evidence than a test showing suspend-and-resume works end to end.

Three options exist:

| Option | What it does | Cost |
| --- | --- | --- |
| Populate an approved static catalog now | Fully implements the declared contract | Maintainers must supply or approve every value |
| Write readiness now and leave capabilities absent | Fixes the blank `READY` column but only partially closes #2764 | The API still promises unreadable capabilities |
| Remove capabilities from the API | Makes the contract honest by shrinking it | Conflicts with the execution plan and requires CRD regeneration |

Based on the execution plan, the first option is the intended end state. The safe next step is to ask maintainers for the authoritative catalog values or agreement on a deliberately limited first catalog. We should not invent 13 fields across 4 adapters, because that creates up to 52 claims users may rely on.

## 7. The code changes required

The exact patch depends on the two decisions above, but the controller plumbing is clear.

### 7.1 Add pure Harness status derivation

Create a semantic function, likely in a new file such as:

```text
go/core/internal/controller/harness.go
```

A **semantic function** computes a result without writing to external systems. Given a Harness and watched dependencies, it should return a desired `HarnessStatus`.

Its responsibilities should be:

1. Copy `harness.Generation` into `ObservedGeneration`.
2. Determine the selected runtime type.
3. Resolve the agreed Harness-owned references.
4. Select the approved capability record.
5. Produce exactly one `Ready` condition with a stable reason and message.

Concrete example: generation 7 selects `codex`, references existing WorkerPool `default`, and all 2 credential references resolve. The function returns `observedGeneration: 7`, the Codex catalog record, and `Ready=True`. If credential 2 disappears, the same function returns `Ready=False` with a missing-reference reason.

This logic should be pure so it can be tested with in-memory KRT collections rather than a Kubernetes cluster.

### 7.2 Add `HarnessStatuses` to the collection graph

Update [`go/core/internal/controller/collections.go`](go/core/internal/controller/collections.go):

1. Add a field similar to:

   ```go
   HarnessStatuses krt.StatusCollection[*kagentv1alpha3.Harness, kagentv1alpha3.HarnessStatus]
   ```

2. Construct it in `NewCollections` after its dependency collections exist.
3. Return it in the `Collections` value.

This creates the desired state but still performs no Kubernetes write.

### 7.3 Add a Harness status queue and handler

Update [`go/core/internal/controller/reconciler.go`](go/core/internal/controller/reconciler.go) by following the existing ModelConfig pattern:

1. Add a `harnessStatuses` queue.
2. Add a `harnessStatusHandler` registration.
3. Compare desired status with stored status and enqueue only real differences.
4. Wait for the handler to synchronize before starting queues.
5. Run the queue until shutdown.

If 100 watched Harnesses already have the desired status, the comparison should enqueue 0 writes. This avoids an endless controller loop where each status write causes another identical write.

### 7.4 Add the Kubernetes status writer

Add a method parallel to `reconcileModelConfigStatus`:

```go
func (r *Reconciler) reconcileHarnessStatus(ctx context.Context, key string) error
```

For key `team-a/codex`, it should:

1. Read the desired derived status.
2. Read the latest observed Harness.
3. deep-copy the object so the informer cache is not mutated; an **informer cache** is the controller's in-memory copy of watched Kubernetes objects, and changing it directly would bypass Kubernetes;
4. preserve or set transition times;
5. skip the write if status is already equal;
6. call `r.status.Harnesses("team-a").UpdateStatus(...)`.

The existing RBAC already permits this call, so no Helm RBAC change should be needed.

### 7.5 Preserve transition times

Add a helper parallel to `modelConfigStatusWithTransitionTimes`.

Suppose `Ready=True` was first written at `10:05`, and the controller reevaluates the same generation 20 times between `10:05` and `10:25`. All 20 comparisons should retain `10:05`. If `Ready` changes to false at `10:26`, only then should the time change to `10:26`.

Without this helper, operators cannot tell whether a condition changed or merely got rewritten.

### 7.6 API and generated files probably remain unchanged

The fields, status subresource, printer column, and RBAC permission already exist. Therefore, a minimal implementation should not need to modify:

- `go/api/v1alpha3/harness_types.go`;
- `go/api/v1alpha3/zz_generated.deepcopy.go`;
- `go/api/config/crd/bases/kagent.dev_harnesses.yaml`;
- `helm/kagent-crds/templates/kagent.dev_harnesses.yaml`;
- Helm RBAC.

An exception is a maintainer decision to change the public condition or capability contract. That would be an API change and would require regenerating artifacts.

## 8. The smallest useful test plan

The tests should prove behavior, not merely increase a coverage number.

### 8.1 Status derivation tests

Add focused tests for the pure status builder, likely in `go/core/internal/controller/harness_test.go`.

Minimum cases after semantics are approved:

1. A valid Harness at generation 4 returns `observedGeneration=4` and `Ready=True`.
2. A missing required reference returns `Ready=False` with the exact expected reason.
3. Each supported runtime type returns the approved capability catalog entry.
4. Updating a dependency changes the derived status without editing the Harness itself.

Case 4 is important. If Secret `runtime-auth` is added while the Harness remains generation 4, KRT must recompute status from false to true. Otherwise the controller would only recover after an unrelated Harness edit.

### 8.2 Writer test

Extend `go/core/internal/controller/reconciler_test.go` with a fake Kubernetes client. A **fake client** is an in-memory test replacement that records API reads and writes without requiring a real cluster:

1. Store one Harness with no status.
2. Supply one desired status object.
3. call `reconcileHarnessStatus`;
4. read the Harness back;
5. assert `Ready=True`, the capability record is present, and `lastTransitionTime` is nonzero.

Then reconcile the identical status a second time and assert the transition time is unchanged.

### 8.3 Collection wiring test

Either extend `go/core/internal/controller/collections_test.go` or make the derivation test use KRT directly. The test should prove a watched dependency change produces a new desired status.

### 8.4 What not to add

For the minimal patch:

- Do not add tests for `kubectl` formatting; the printer column already exists.
- Do not duplicate the gRPC test that translates a `Ready` condition to a Boolean; that behavior is already covered.
- Do not regenerate the CRD when no API source changed.
- Do not introduce a new abstraction around all status types unless Harness status reveals actual shared behavior beyond transition-time handling.

A full cluster end-to-end test is optional for this narrow wiring change because no new field or public endpoint is introduced. Maintainers may still request one because the final symptom is visible through Kubernetes.

## 9. Recommended implementation sequence

Order matters because it keeps failures understandable.

1. **Confirm semantics with a maintainer.** Define `Ready` and obtain approved capability values. Coding before this risks a technically clean implementation of the wrong contract.
2. **Write derivation tests.** These force the agreed semantics into concrete inputs and outputs.
3. **Implement pure derivation.** At this point no external writes exist, so failures are local and easy to inspect.
4. **Wire `HarnessStatuses` into `Collections`.** Prove dependency changes recompute desired status.
5. **Add the queue, handler, writer, and transition-time logic.** This is the smallest point at which Kubernetes can change.
6. **Add the focused writer test.** Prove an empty status becomes populated and identical reconciliations do not churn it.
7. **Let the user run formatting and tests.** Under the current workspace rules, the assistant must not run builds, tests, formatters, dependency installation, `make`, or Go commands.

The likely user-run verification commands, after implementation, are:

```bash
gofmt -w go/core/internal/controller/<changed-go-files>
cd go
go test ./core/internal/controller -count=1
```

Broader repository checks can follow if the focused test passes. These commands require a suitable Go/Linux development environment; the user must run them because this assistant is restricted to native Windows inspection and file editing.

Before writing code, the focused maintainer question should be:

> I traced the existing status paths. The API, printer column, gRPC projection, status subresource, and RBAC already exist; only Harness status derivation and writing are missing. I propose `Ready=True` when the adapter is registered and Harness-owned references resolve, with pair-specific compilation remaining in AgentTemplate status. The issue mentions `Accepted`, but existing consumers read `Ready`. Should this PR publish only `Ready`, and what versioned capability values should be authoritative for kagent, Codex, Claude, and BYO?

## 10. A worked example of the finished behavior

Assume namespace `team-a` contains:

- Harness `codex`, generation 5;
- WorkerPool `default`;
- Secret `codex-auth` with key `token`;
- an approved Codex capability catalog entry, version `v1`.

The Harness owns 2 external references: the WorkerPool and Secret. Both resolve, so the controller writes something conceptually like:

```yaml
status:
  observedGeneration: 5
  capabilities:
    version: v1
    streaming: true
    inputRequired: true
    checkpoint: false
    # Other approved fields omitted from this example for readability.
  conditions:
    - type: Ready
      status: "True"
      reason: Ready
      message: Harness dependencies and runtime adapter are ready
      observedGeneration: 5
      lastTransitionTime: "2026-09-16T10:05:00Z"
```

Then three causal effects follow:

1. The CRD printer reads the condition, therefore `kubectl get harness` displays `READY=True`.
2. The gRPC service reads the same condition, therefore `ListHarnesses` returns `ready: true`.
3. Documentation or tooling reads `capabilities`, therefore it does not need a separately maintained guess about Codex behavior.

If the Secret is deleted at `10:20`, the dependency collection changes, KRT recomputes desired status, the handler enqueues `team-a/codex`, and the writer changes `Ready` to false. The Harness generation can remain 5 because the Harness spec did not change; only its environment did.

## 11. Risks, tradeoffs, and open questions

| Risk, tradeoff, or question | Why it matters | What we do about it |
| --- | --- | --- |
| `Ready` has no precise definition yet | A condition that means “valid YAML” is too weak, while one that promises every future agent will run is impossible | Ask maintainers to approve a Harness-owned-reference definition before coding |
| The issue says `Accepted`, while code consumers read `Ready` | Implementing the issue wording literally would not fix the blank `READY` column or false gRPC value | Confirm that `Ready` is the required condition, and add `Accepted` only if maintainers deliberately expand the contract |
| Capability values do not exist in code | Guessing produces user-visible promises that may be false across 4 adapters and roughly 13 fields | Obtain an authoritative, versioned catalog or explicitly split capability publication into an approved follow-up |
| Dependency changes must trigger reconciliation | A missing Secret may appear later without changing Harness generation | Build status as a KRT-derived collection that fetches dependencies, then test false-to-true recovery |
| Status writes can create loops | Writing status triggers another watch event | Compare desired and current status, preserve transition times, and skip identical writes |
| Harness readiness can be confused with AgentTemplate readiness | A shared Harness may be sound while one specific template is invalid | Keep Harness-owned checks in Harness status and pair-specific compilation checks in AgentTemplate status |
| Capability truth may depend on conformance work not yet complete | Source code presence does not prove lifecycle behavior works end to end | Publish only values backed by the repository's approved conformance evidence |
| Expanding scope into CRD redesign increases review cost | The current API, printer column, subresource, and RBAC are already present | Keep the implementation inside controller derivation and writing unless maintainers explicitly change the contract |

The core bug is simple: the controller never writes `Harness.status`. The careful part is defining truthful readiness and capability data before adding the otherwise straightforward status pipeline.
