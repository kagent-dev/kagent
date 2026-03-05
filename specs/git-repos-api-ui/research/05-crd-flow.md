# CRD → Controller → DB → API → UI Flow

## Full Pipeline (Agent as reference)

```
UI Form → POST /api/agents
  → HandleCreateAgent() → Creates K8s Agent CRD
    → AgentController watches → kagentReconciler
      → adkTranslator.TranslateAgent() → K8s manifests
      → reconcileDesiredObjects() → applies to cluster
      → upsertAgent() → stores to DB
      → reconcileAgentStatus() → updates CRD status
  → Response to UI
```

## 1. CRD Definition (`go/api/v1alpha2/agent_types.go`)
- Types with kubebuilder markers, JSON tags
- `AgentSpec` (desired), `AgentStatus` (observed)
- `make -C go generate` produces CRD manifests

## 2. Controller (`go/internal/controller/agent_controller.go`)
- Watches Agent CRD + related resources (ModelConfig, RemoteMCPServer)
- Delegates to shared `kagentReconciler`

## 3. Shared Reconciler (`go/internal/controller/reconciler/reconciler.go`)
- `ReconcileKagentAgent()`: fetch → translate → reconcile objects → store DB → update status
- Two status conditions: `Accepted` and `Ready`
- Handles deletion via finalizers → `DeleteAgent()` from DB

## 4. Translator (`go/internal/controller/translator/agent/`)
- CRD → K8s manifests (Deployment, Service, ConfigMap)
- CRD → `adk.AgentConfig` for DB storage

## 5. Database
- `database.Agent{ID, Type, Config}` — JSON config
- ID = Python identifier from namespace/name
- Upsert via GORM OnConflict

## 6. HTTP API (`go/internal/httpserver/handlers/agents.go`)
- CRUD handlers read/write K8s CRDs directly
- List/Get: reads from K8s API, enriches with status
- Create/Update: writes K8s CRD → triggers controller

## 7. UI
- Server actions → fetchApi → HTTP handlers
- Types mirror backend responses

## Key Insight
For CRD-backed entities, the **API handlers talk to K8s** (not DB directly).
The DB is populated by the **controller** during reconciliation.
Some entities (Sessions, Tasks) are DB-only (no CRD).

## Decision Point for Git Repos
- **CRD-backed:** Full pipeline (CRD → controller → DB → API → UI)
- **DB-only:** Simpler (API → DB → UI), no K8s controller needed
- Choice depends on whether git repos need K8s-native lifecycle management
