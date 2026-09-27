# Issue #2737 explained from zero: identifying an agent on outbound model requests

This document explains [kagent issue #2737](https://github.com/kagent-dev/kagent/issues/2737), why the apparently small request hides several design choices, and what solution should be proposed before implementation.

The code observations below were checked against local commit `7471879e16f6ea68fbe183d9f9ee19976ec6836d` from 2026-09-20.

## 0. The big picture

A kagent agent sends prompts to a **large language model**, abbreviated **LLM**. An LLM is a service such as OpenAI, Anthropic, Gemini, or an internally hosted model that accepts a request and produces a model response.

Many organizations do not let every agent call an LLM provider directly. They put a **gateway** between the agent and the provider. A gateway is an HTTP server that receives the request, records or controls it, and then forwards it to the actual provider.

For example, suppose three agents make model calls during one hour:

- `payments/refund-agent` uses 80,000 tokens.
- `support/ticket-agent` uses 15,000 tokens.
- `platform/incident-agent` uses 5,000 tokens.

The gateway can already count 100,000 total tokens. The problem in issue #2737 is that it cannot necessarily attribute those tokens to the three agents. All three requests can use the same model route and the same credentials, so they look identical at the gateway.

The proposed missing piece is an HTTP header:

```http
X-Agent-Id: payments/refund-agent
```

If the gateway copies that value into its usage records, it can calculate that the refund agent used 80% of the 100,000 tokens.

That sounds like a one-line change. It is not, because kagent has more than one thing that could reasonably be called an "agent ID," and those identities exist at different stages of the request's life.

## 1. Foundation: HTTP requests and headers

**HTTP**, or Hypertext Transfer Protocol, is the request-and-response protocol commonly used between software services.

An HTTP request contains:

1. A method, such as `POST`.
2. A destination, such as `/v1/chat/completions`.
3. Headers, which are small named metadata values.
4. An optional body, which contains the main data.

A simplified model request looks like this:

```http
POST /v1/chat/completions HTTP/1.1
Host: models.example.com
Authorization: Bearer secret-token
Content-Type: application/json

{"model":"gpt-5","messages":[{"role":"user","content":"Summarize this incident"}]}
```

`Authorization` and `Content-Type` are headers. The JSON object after the blank line is the body.

Header names are case-insensitive. `X-Agent-Id`, `x-agent-id`, and `X-AGENT-ID` denote the same HTTP header. Code checking for conflicts must therefore compare normalized names rather than exact spelling.

### Concrete example 1

Assume two requests contain the same model and prompt:

```http
X-Agent-Id: payments/refund-agent
```

and:

```http
X-Agent-Id: support/ticket-agent
```

If the first response uses 800 tokens and the second uses 200, the gateway can record:

```text
payments/refund-agent = 800 tokens
support/ticket-agent  = 200 tokens
```

Without the header, it can record only:

```text
unknown agent = 1,000 tokens
```

The header does not change what the model generates. It changes what the gateway can attribute.

## 2. Foundation: metrics, labels, and cardinality

A **metric** is a numerical measurement recorded over time. Examples are request count, latency in milliseconds, and token count.

A **label** is a field used to divide a metric into groups. If `agent_id` is a label, the same token metric can be grouped by agent.

**Cardinality** means the number of distinct label combinations. High cardinality consumes more memory and storage in monitoring systems.

### Concrete example 2

Suppose a cluster has:

- 20 agent templates;
- 3 model names;
- 2 token types: input and output.

Using the template as the `agent_id` can create at most:

```text
20 agents × 3 models × 2 token types = 120 time series
```

Now suppose the system creates 10,000 short-lived agent instances per day. Using the instance UUID as `agent_id` can create:

```text
10,000 instances × 3 models × 2 token types = 60,000 time series per day
```

The second choice is much more expensive. This is why "which ID?" is an operational decision, not just a naming detail.

## 3. Foundation: the four relevant kagent objects

Current kagent separates authored behavior, runtime implementation, live state, and model configuration.

### 3.1 `AgentTemplate`

An `AgentTemplate` is a Kubernetes custom resource describing an agent's behavior. A **custom resource** is an object added to Kubernetes by an application instead of being built into Kubernetes itself.

An `AgentTemplate` can contain a system prompt, model reference, tools, and child-agent bindings.

Example identity:

```text
namespace: payments
name: refund-agent
```

The namespaced name is therefore:

```text
payments/refund-agent
```

Two namespaces may each contain an `assistant`, so `assistant` alone is not globally unique:

```text
payments/assistant
support/assistant
```

### 3.2 `Harness`

A `Harness` describes how an `AgentTemplate` is executed. Current release-blocking harnesses include kagent, Claude, and Codex.

The same template could theoretically run through two harnesses:

```text
payments/refund-agent + kagent harness
payments/refund-agent + claude harness
```

The behavior identity remains `payments/refund-agent`; the runtime implementation differs.

### 3.3 `AgentInstance`

An `AgentInstance` is one live, stateful realization of an `AgentTemplate` and `Harness` pair. It is stored in PostgreSQL and identified by a UUID, which is a randomly generated 128-bit identifier.

Two live instances of the same template might be:

```text
0199a111-1111-7111-8111-111111111111
0199a222-2222-7222-8222-222222222222
```

Both can execute `payments/refund-agent`. An instance ID answers "which live copy made this call?" A template identity answers "which authored agent behavior made this call?"

### 3.4 `ModelConfig`

A `ModelConfig` describes how to reach and configure a model. It can specify a provider, model name, base URL, credentials, and default HTTP headers.

One `ModelConfig` can be reused by many templates:

```text
ModelConfig: shared-gpt-5
  used by payments/refund-agent
  used by support/ticket-agent
  used by platform/incident-agent
```

This reuse is desirable because an operator can update one model route instead of copying the same settings three times.

Issue #2737 explicitly rejects making users put the agent header in `ModelConfig.defaultHeaders`: doing that would require three otherwise identical `ModelConfig` objects merely to carry three different agent values.

## 4. Foundation: compile time versus request time

**Compile time** here means the controller turns an `AgentTemplate`, `Harness`, and referenced resources into immutable runtime configuration.

**Request time** means a running agent is about to send one specific HTTP request to a model.

At compile time, kagent knows the `AgentTemplate`:

```text
payments/refund-agent
```

At runtime, the same compiled revision may back multiple `AgentInstance` objects:

```text
instance A = 0199a111-...
instance B = 0199a222-...
```

Current actor creation selects a prepared `ActorTemplate`; it does not provide a fresh per-instance model-header map. Therefore a template identity can be compiled into static headers cheaply, while an instance UUID would require new dynamic propagation.

### Concrete example 3

Suppose the controller compiles one revision at 10:00 with:

```http
X-Agent-Id: payments/refund-agent
```

At 10:05, instance A starts from that revision. At 10:10, instance B starts from the same revision. Both correctly report the same behavioral identity.

If the header instead had to contain instance A's UUID, the shared revision could not also be correct for instance B. A dynamic value would need to enter the runtime after the instance is created and be attached separately to every model call.

## 5. What the current code does

The current path has four important stages.

### 5.1 The builder compiles each template separately

`go/core/internal/translator/adkconfig/builder.go` contains `compileAgent`. It receives an `AgentInput` containing the current `AgentTemplate` and its resolved `ModelConfig`.

The function also recursively compiles child agents. If a root agent has two child templates, `compileAgent` runs three times: once for the root and once for each child.

This is important because the function has exactly the pair needed for this issue:

```text
current AgentTemplate + current model configuration
```

### 5.2 Model translation copies `defaultHeaders`

`go/core/internal/translator/adkconfig/model.go` translates the Kubernetes `ModelConfig` into a provider-specific runtime model.

For supported provider types, the code currently performs the equivalent of:

```go
Headers: model.Spec.DefaultHeaders
```

That runtime model is serialized into `KAGENT_CONFIG_JSON`. Both the Python and Go declarative runtimes read the same logical header map.

### 5.3 The Python runtime passes those headers to model clients

`python/packages/kagent-adk/src/kagent/adk/types.py` reads:

```python
extra_headers = model_config.headers or {}
```

It then supplies those headers to model clients, for example as `default_headers` for an OpenAI-compatible client.

If the compiled map contains two entries—`X-Tenant: acme` and `X-Agent-Id: payments/refund-agent`—the OpenAI-compatible client receives both entries.

### 5.4 The Go runtime injects those headers through its HTTP transport

`go/adk/pkg/models/base.go` contains `headerTransport.RoundTrip`. A **transport** is the component that sends an HTTP request.

Before forwarding a request, it clones the request and sets every configured header:

```go
for k, v := range t.headers {
    req.Header.Set(k, v)
}
```

Therefore the existing runtime plumbing can already carry the proposed header for model types that honor `BaseModel.Headers`. The missing work is primarily associating the correct template identity with the correct compiled model.

## 6. Two existing headers that do not solve this issue

### 6.1 `x-kagent-agent-instance-id`

`go/api/a2a/routing.go` defines:

```text
x-kagent-agent-instance-id
```

This header selects which `AgentInstance` should receive an incoming Agent-to-Agent request. **A2A**, or Agent-to-Agent, is the protocol used to send tasks and messages to agents.

Direction matters:

```text
client -> kagent gateway -> AgentInstance
```

Issue #2737 concerns the opposite kind of traffic:

```text
running agent -> model gateway -> model provider
```

The routing header exists, but it is not automatically copied onto outbound model calls.

### 6.2 `X-Agent-Name`

`python/packages/kagent-adk/src/kagent/adk/_token.py` adds `X-Agent-Name` to requests made by the Python runtime's controller client.

Those requests are control-plane calls to kagent services. They are not the model client's OpenAI, Anthropic, or Gemini requests. Reusing the same words does not make the request paths the same.

## 7. The issue in one causal chain

1. Three `AgentTemplate` objects may share one `ModelConfig`.
2. That model configuration sends all three agents through one gateway route.
3. The gateway observes token counts, but the requests contain no automatic template identity.
4. The gateway therefore cannot divide 100,000 tokens into 80,000, 15,000, and 5,000 per agent.
5. Putting a literal header in the shared `ModelConfig` would give all three agents the same value.
6. Copying the `ModelConfig` three times would work, but would make configuration ownership worse.
7. Therefore kagent should derive the header from the `AgentTemplate` while compiling each agent's runtime model.

## 8. The decisions hidden inside “send the agent ID”

### Decision 1: Which object is the agent?

There are four plausible values:

| Candidate | Example | Benefit | Cost |
| --- | --- | --- | --- |
| Template name | `refund-agent` | Readable and stable | Collides across namespaces |
| Namespaced template name | `payments/refund-agent` | Readable, stable, and unique inside one cluster | Not globally unique across clusters |
| Template Kubernetes UID | `b742...` | Unique across delete/recreate | Unreadable and produces a new metric series after recreation |
| AgentInstance UUID | `0199a111-...` | Identifies one live copy exactly | High cardinality and unavailable in the shared compiled revision |

**Recommendation:** use the namespaced `AgentTemplate` name, such as `payments/refund-agent`.

This matches the stated goal, "models usage per agent," while keeping the number of identities close to the number of authored agents. If 20 templates each create 500 instances, this recommendation creates 20 identity values rather than 10,000.

If cross-cluster aggregation is required, the monitoring pipeline can add an existing cluster label. For example:

```text
cluster=prod-eu, agent_id=payments/refund-agent
cluster=prod-us, agent_id=payments/refund-agent
```

That is clearer than embedding the cluster into one opaque ID.

### Decision 2: What should the header be called?

The issue asks for `X-Agent-Id`. The current repository does not define that name as an outbound model-request contract.

**Recommendation for the issue proposal:** keep the requested spelling, `X-Agent-Id`, but explicitly ask the maintainer to confirm that this is the AgentGateway contract. Do not silently rename it during implementation.

A kagent-specific alternative such as `X-Kagent-Agent-Template` would be less collision-prone, but AgentGateway would then need to understand that different name. Interoperability is more important than inventing the most aesthetically precise header.

### Decision 3: Who wins if the user already configured that header?

HTTP header names are case-insensitive, so these conflict:

```yaml
defaultHeaders:
  x-agent-id: manually-chosen-value
```

and:

```http
X-Agent-Id: payments/refund-agent
```

Silently accepting the manual value allows incorrect attribution. Silently overwriting it hides a configuration mistake.

**Recommendation:** treat `X-Agent-Id` as system-owned and reject a case-insensitive collision with a clear validation error. For example:

```text
ModelConfig defaultHeaders must not set reserved header "X-Agent-Id"
```

This makes the incorrect state visible rather than selecting a winner invisibly.

### Decision 4: Which harnesses are covered?

The kagent harness uses the shared ADK configuration consumed by the Python and Go declarative runtimes. That path already supports model header maps.

The Claude and Codex harness compilers currently reject `ModelConfig.defaultHeaders`. Their model calls are also made by external CLI runtimes rather than the same ADK model clients.

**Recommendation:** scope the first PR to the kagent harness and say so. A five-file cross-harness change that pretends the runtimes have the same transport would be harder to verify and easier to get wrong.

### Decision 5: Should the identity be static or dynamic?

A static template identity can be compiled once. A dynamic instance identity must be introduced after an instance exists and carried through every model invocation.

**Recommendation:** make template attribution static in this issue. If per-instance cost attribution is later required, track it separately with an explicit cardinality decision.

### Decision 6: Does the header reach the external provider?

Default model headers are ordinary outbound headers. If the configured base URL is an internal AgentGateway, that gateway can consume the header. If the base URL points directly to `api.openai.com`, the identifier may be sent to OpenAI.

Example:

```text
internal route: agent -> gateway.corp -> OpenAI
direct route:   agent -> api.openai.com
```

In the first route, the intended gateway sees `payments/refund-agent`. In the second route, the external provider may see it.

This is not a secret, but it is metadata disclosure. The proposal should ask whether kagent intentionally sends the header on every model request or only when the destination is an identified gateway. The current API has no explicit “this endpoint is AgentGateway” flag, so destination-specific behavior would require an additional design.

## 9. Recommended implementation shape

The narrow implementation should derive a header map at the `AgentTemplate` compilation boundary.

### Step 1: define the semantic operation

A **semantic operation** is a small, deterministic function that computes a value without performing network or filesystem input/output.

Conceptually:

```go
func modelHeadersForTemplate(
    existing map[string]string,
    namespace string,
    templateName string,
) (map[string]string, error)
```

Given:

```text
existing     = {"X-Tenant": "acme"}
namespace    = "payments"
templateName = "refund-agent"
```

it returns a new map:

```text
{
  "X-Tenant": "acme",
  "X-Agent-Id": "payments/refund-agent"
}
```

It must return a new map rather than modifying `ModelConfig.Spec.DefaultHeaders` in place. The same resolved `ModelConfig` may be compiled for three templates. Mutating the shared map while compiling the first template could incorrectly leak `payments/refund-agent` into the support agent.

### Step 2: apply it inside `compileAgent`

`compileAgent` has the current template and current resolved model. Apply the derived headers before translating that model into runtime configuration.

This placement produces the correct result even when one root has child agents:

```text
root model header  = X-Agent-Id: support/triage-agent
child model header = X-Agent-Id: support/search-agent
```

If the child makes 300 of a total 1,000 tokens, the gateway can attribute 300 to the child rather than assigning all 1,000 to the root.

### Step 3: reuse the existing runtime header path

Do not add separate Python and Go environment variables merely to reconstruct the same static value twice.

The existing configuration path already carries model headers:

```text
AgentTemplate identity
  -> compiled model headers
  -> KAGENT_CONFIG_JSON
  -> Python or Go model client
  -> outbound HTTP request
```

One compiler change therefore serves both declarative runtimes for provider adapters that already honor headers.

### Step 4: keep generated APIs unchanged

This solution does not require a new `AgentTemplate`, `ModelConfig`, or protobuf field. The identity already exists, and the runtime header map already exists.

Avoiding a public field matters because a public field would create an operator choice where kagent can derive the answer. For 20 templates, asking operators to set 20 matching IDs creates 20 opportunities for spelling drift.

## 10. Tests that prove the behavior

The smallest useful tests are not merely “the helper returned a string.” They must prove shared configuration remains isolated.

### Test 1: preserve ordinary user headers

Input:

```text
{"X-Tenant": "acme"}
```

Expected output:

```text
{"X-Tenant": "acme", "X-Agent-Id": "payments/refund-agent"}
```

### Test 2: reject a case-insensitive reserved-header collision

Input:

```text
{"x-agent-ID": "fake"}
```

Expected error:

```text
ModelConfig defaultHeaders must not set reserved header "X-Agent-Id"
```

### Test 3: do not mutate the source map

Start with one source map containing one entry. After compiling two templates, assert:

```text
source map entries = 1
first model agent header = team-a/agent
second model agent header = team-b/agent
```

If the source map ends with two entries or the second model reports `team-a/agent`, the compiler leaked derived state.

### Test 4: distinguish root and child templates

Compile this tree:

```text
support/triage-agent
└── support/search-agent
```

Assert that the root and child model configurations contain different identity values.

### Test 5: prove an actual HTTP request carries the header

Use a local HTTP test server and make one OpenAI-compatible request. The server should observe exactly one value:

```text
X-Agent-Id: payments/refund-agent
```

The repository already tests generic header injection in the Go model transport. A focused compiler-to-serialized-config test may be sufficient for the first PR, but an HTTP-level test gives stronger evidence if it can reuse existing fixtures without broad setup.

## 11. Should we propose a solution before coding?

Yes. For this issue, post a precise solution comment before coding because the issue is only a few sentences long and does not answer the identity, scope, precedence, or disclosure questions above.

Your normal process—post the solution, start implementing, then open the PR—is reasonable. It saves maintainer time when the proposal is concrete and the first patch is narrow. You do not necessarily need to wait for a response before beginning a reversible implementation.

The important boundary is this:

- Start the compile-time, namespaced-template implementation after posting the comment.
- Do not build dynamic per-`AgentInstance` propagation without maintainer confirmation.
- Keep the first PR scoped to the kagent harness.
- Open it as a draft if the header name or external-provider behavior remains unconfirmed.

This gives maintainers code to review without making them reverse-engineer hidden assumptions.

## 12. Ready-to-post issue comment

```markdown
I’d like to work on this. I traced the current v1alpha3 path and propose the following narrow first PR.

**Identity:** treat “agent ID” as the namespaced `AgentTemplate` identity, for example `payments/refund-agent`, rather than the `AgentInstance` UUID. The template identity is available when the runtime revision is compiled and keeps the metrics label bounded; an instance UUID would require new request-time propagation and could create one time series per short-lived instance.

**Implementation:** in `go/core/internal/translator/adkconfig`, derive a fresh model-header map while compiling each `AgentInput` and add:

```http
X-Agent-Id: <namespace>/<agent-template-name>
```

This is done per compiled template, not in `ModelConfig`, so multiple templates can continue sharing one `ModelConfig`. Child AgentTemplates will receive their own identity. The existing `BaseModel.Headers` -> Python/Go model-client path will carry the value, so this should not need a CRD or protobuf change.

I’ll avoid mutating `ModelConfig.Spec.DefaultHeaders`, preserve unrelated custom headers, and reject a case-insensitive user collision with the reserved `X-Agent-Id` header instead of silently producing incorrect attribution. Tests will cover shared ModelConfigs, root/child identities, map immutability, and the serialized runtime configuration.

I plan to scope the first PR to the kagent harness; Claude and Codex use different model transports and currently reject `ModelConfig.defaultHeaders`.

Two contract points I’ll call out in the draft PR unless confirmed here:

1. Is `X-Agent-Id` the exact header AgentGateway intends to consume?
2. Should this header be sent on every model request, including direct provider endpoints, or only to an identified gateway endpoint? The current ModelConfig API does not distinguish those destinations.

If the intended identity is instead the per-live-instance UUID, I’ll stop before expanding the scope because that needs a different request-time design.
```

## 13. Recommended work sequence

1. Post the comment above. This takes about 2 minutes.
2. Add focused failing tests for shared `ModelConfig` isolation and root/child identity. This establishes the intended contract before implementation.
3. Add the pure header-derivation helper with case-insensitive collision detection.
4. Integrate it at `adkconfig.Builder.compileAgent` before model translation.
5. Inspect the serialized `AgentConfig` for the root and child models.
6. Ask the user of this workspace to run the focused Go tests and provide the output; local working agreements prohibit the agent from running tests or generation commands.
7. Open a draft PR if the two contract questions remain unanswered; otherwise open a normal PR.

The order matters. If the maintainer changes the identity contract after step 1, only a small helper and focused tests need adjustment. If dynamic instance propagation were implemented first, changes would spread through the control plane, actor lifecycle, and both runtimes before the basic term “agent ID” was settled.

## 14. Risks, tradeoffs, and open questions

| Risk or question | Why it matters | What to do about it |
| --- | --- | --- |
| “Agent ID” could mean template or instance | The two values have different lifetimes and implementation paths | State that the first PR uses `<namespace>/<AgentTemplate name>` and stop if maintainers require instance UUIDs |
| `X-Agent-Id` may not be AgentGateway’s final contract | A perfectly transmitted header is useless if the gateway reads another name | Ask for explicit confirmation in the issue comment and centralize the header name in one constant |
| Instance UUID labels can have high cardinality | 10,000 instances can create tens of thousands of metric series | Use the bounded template identity for this issue |
| A user may already set `x-agent-id` manually | Silent override or spoofing would make attribution untrustworthy | Detect conflicts case-insensitively and return a clear validation error |
| Mutating the shared `ModelConfig` map can leak one identity into another agent | Three templates can reuse one resolved object | Clone the map before adding the derived header and test source-map immutability |
| Child agents can call models independently | Assigning every call to the root hides which behavior consumed tokens | Inject the identity during each recursive `compileAgent` call |
| The header may reach an external provider | `payments/refund-agent` reveals internal naming metadata | Confirm whether universal transmission is acceptable or whether a gateway-specific signal is needed |
| Claude and Codex have different transports | Pretending all harnesses share ADK headers would produce partial or untested behavior | Scope the first PR to the kagent harness and track other harnesses separately if required |
| Some provider adapters do not currently honor generic headers equally | A compiler test can pass while one provider drops the header | State the supported provider path, begin with the OpenAI-compatible gateway case, and add transport-level coverage where needed |
| The issue author marked willingness to contribute | Two contributors could start the same work | Post the claim and design comment before editing, then link the PR quickly |

## Plain-words conclusion

Issue #2737 is asking kagent to put an automatic identity tag on each model request so a gateway can say which authored agent consumed the tokens.

The recommended first version is:

```http
X-Agent-Id: <namespace>/<AgentTemplate name>
```

It should be derived while each template's model configuration is compiled, copied into the existing runtime model-header map, and treated as a reserved system header. This is small enough to begin after posting a design comment, but the header name and external-provider behavior should remain explicit review questions.
