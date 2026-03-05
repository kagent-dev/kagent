# Requirements

## Questions & Answers

### Q1: What is the core concept — should a user be able to define a cron schedule that triggers an AI agent (kagent) to run a prompt at specified intervals, similar to how Kubernetes CronJobs run containers on a schedule?

**A1:** Yes, exactly — a CRD that schedules prompt execution on a cron.

### Q2: Which existing kagent resource should the AI CronJob reference — should it point to an existing `Agent` CR (v1alpha2) so the scheduled prompt runs against a fully configured agent with its model, tools, and system prompt?

**A2:** Yes, it should reference an existing Agent CR.

### Q3: What should happen with the output of each scheduled run? Options to consider:
- Store results in the CRD status (simple, but limited size)
- Create a child "Run" CR per execution with results (auditable history)
- Write to an external sink (ConfigMap, Secret, webhook, S3, etc.)
- Just log it (simplest, but hard to query)

Which approach, or combination, makes sense for your use case?

**A3:** It will be stored in a session, same as current agent runs. Each scheduled execution creates a new session in the existing database.

### Q4: Should the CRD support standard Kubernetes CronJob semantics like concurrency policy (Allow / Forbid / Replace), suspend, starting deadline seconds, and history limits — or should we start with a minimal spec (just schedule + prompt + agent ref) and add those later?

**A4:** Start minimal — just schedule, prompt, and agent ref. Advanced CronJob semantics can be added later.

### Q5: How should the controller trigger the agent run — should it call the existing kagent HTTP server API (the same endpoint the UI uses to create a session and send a message), or should it invoke the agent runtime directly?

**A5:** Through the HTTP server API, same as the UI uses.

### Q6: Should the CRD status track execution history — e.g., last run time, last result (success/failure), next scheduled run, and session ID of the most recent execution?

**A6:** Yes — track last run time, success/failure, next scheduled run, and session ID of the most recent execution.

### Q7: Should the UI have visibility into AI CronJobs — e.g., a page to list scheduled jobs, see their status, view past sessions triggered by them — or is this purely a CRD/kubectl-managed feature for now?

**A7:** Yes, there is already a placeholder page in the UI for CRUD operations on AI CronJobs.

### Q8: What's the CRD name — `AgentCronJob`, `ScheduledTask`, `AgentSchedule`, or something else? And should it live in the `kagent.dev` API group under `v1alpha2`?

**A8:** `AgentCronJob` in the `kagent.dev` API group, `v1alpha2`.

### Q9: Should the controller handle error scenarios like the referenced Agent CR not existing, or the HTTP server being unreachable? For example: set status to failed with an error message and retry on the next scheduled tick, or requeue with backoff?

**A9:** Set status to failed with an error message and retry on the next scheduled tick — no immediate requeue/backoff.

### Q10: Does the prompt field need to support templating or variable substitution (e.g., injecting current date/time, namespace, or other dynamic values), or is it a static string for now?

**A10:** Static string for now. Templating can be added later.

### Q11: Should the HTTP server expose new endpoints for managing AgentCronJobs (list, create, update, delete) — to back the existing UI placeholder — or should the UI talk to the K8s API directly for CRUD and only the scheduled execution goes through the HTTP server?

**A11:** HTTP server should expose CRUD endpoints for AgentCronJobs (list, create, update, delete) to back the UI.

