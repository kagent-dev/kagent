# EP-89405: File Upload & Artifact Support in Chat

* Issue: [MSFN-89405] (internal) — feat: file upload feature
* Status: `implemented` (branch `feature/file-upload-artifacts`, commit `ccfada50`)
* Related: EP-2046 (Chat UI for MCP UI widgets) — shares the chat files but
  explicitly excludes the file-upload backend, which this EP owns.

## Background

kagent's chat could only exchange text. Users frequently need to hand an agent a
file (a log, a PDF, a CSV, a screenshot) and to receive files an agent/tool
produces (a generated report, an extracted table). The Go ADK runtime ships an
artifact subsystem (`artifact.Service`, `loadartifactstool`, the
`SaveInputBlobsAsArtifacts` run option, and the per-event `ArtifactDelta`
signal), but kagent serves agents purely over A2A (`adka2a`) and never wired the
`ArtifactService` into the runner — so none of it was reachable.

This EP enables an **end-to-end file upload / download round trip** in chat:

1. Users attach files in the chat UI; they travel inline (base64) over the
   existing A2A message/SSE channel — **no new HTTP API, no CRD field**.
2. The Go ADK executor persists inbound uploads as artifacts and surfaces
   agent-produced artifacts back to the UI as downloadable A2A file parts.
3. Agents get a `save_artifact` tool (produce files) plus the built-in
   `load_artifacts` tool (reference uploaded/produced files across turns).
4. Both runtimes extract text from uploaded files (notably PDF) so models that
   cannot natively read a document still receive its content.

## Motivation

- Let users give agents real working material instead of pasting text.
- Let agents return generated files (reports, transformed data) as first-class,
  downloadable chat attachments rather than dumping content into the message.
- Reuse the ADK's existing, battle-tested artifact contract rather than inventing
  a parallel storage/transport.

### Goals

- Wire the ADK in-memory `ArtifactService` into the Go runner (process-lifetime
  persistence, versioned, user/session scoped).
- Accept inbound A2A file parts, persist them via `SaveInputBlobsAsArtifacts`,
  and pass them inline to the model.
- Emit agent-saved artifacts back to the UI as A2A `FilePart` events, driven by
  the `ArtifactDelta` event signal (event-driven, no store diffing).
- Register `loadartifactstool` (load) and a new `save_artifact` tool (produce)
  for agents.
- Extract text from uploaded documents (PDF first) in both the Go ADK
  (`fileextract`) and Python (`_file_extract`) model paths so non-multimodal
  models still receive document content.
- Chat UI: attach multiple files with type/size validation; render image
  thumbnails and downloadable file chips for both user and agent bubbles.
- Raise the nginx/proxy request-body limit so uploads aren't rejected at the edge.

### Non-Goals

- Durable / shared artifact storage (GCS, DB) and cross-replica access. Artifacts
  live in process memory and are lost on pod restart.
- A standalone artifact browser UI (list / delete / version history) beyond the
  per-message chips.
- A dedicated artifact HTTP/storage API for the UI (everything rides A2A).
- The MCP-app / minimap chat features that share the same chat files (EP-2046).

## Implementation Details

### Transport & data model

- **Upload:** `@a2a-js/sdk` `FilePart` `{ kind: "file", file: { name, mimeType,
  bytes /*base64*/ } }` appended alongside the text part on `message/stream`.
- **Download:** A2A artifact-update events carrying a `FilePart`.
- **ADK artifact value:** `*genai.Part` with `InlineData {Data, MIMEType,
  DisplayName}`; key `(AppName, UserID, SessionID, FileName, Version)`; versions
  auto-increment; `ArtifactDelta map[filename]version` set automatically on save.

### Go ADK runtime

- **`go/adk/pkg/runner/adapter.go`** — set
  `ArtifactService: adkartifact.InMemoryService()` on the `runner.Config` in
  `CreateRunnerConfig` (single instance, reused for process-lifetime persistence).
- **`go/adk/pkg/agent/agent.go`** — register `loadartifactstool.New()` and the
  new `save_artifact` tool in the agent's local tool set.
- **`go/adk/pkg/tools/save_artifact_tool.go`** — new tool letting the LLM produce
  a downloadable file from chat; the executor surfaces it as an A2A file part.
- **`go/adk/pkg/a2a/executor.go`** — enable
  `runConfig.SaveInputBlobsAsArtifacts = true`; on each event with
  `Actions.ArtifactDelta`, `Load` each `(name, version)` from the store, set
  `InlineData.DisplayName`, convert via `ToA2APart`, and emit an A2A
  artifact-update `FilePart` event (`LastChunk=true`). Load/convert errors are
  logged and skipped so the turn continues.
- **`go/adk/pkg/a2a/artifacts.go`** — helpers for building/emitting artifact
  events from saved ADK parts.
- **`go/adk/pkg/fileextract/`** (`fileextract.go`, `pdf.go`) — extract text from
  uploaded documents (PDF and other supported types) so the content is injected
  for models that can't read the raw file.
- **`go/adk/pkg/models/openai_adk.go`** — inject extracted file text into the
  OpenAI request path.
- New Go deps in `go/go.mod` / `go/go.sum` for PDF extraction.

### Python runtime

- **`python/packages/kagent-adk/src/kagent/adk/models/_file_extract.py`** — text
  extraction (PDF, etc.) mirroring the Go path.
- **`python/packages/kagent-adk/src/kagent/adk/models/_openai.py`** — inject
  extracted file content into the OpenAI request.
- **`python/packages/kagent-adk/pyproject.toml`** — add the extraction dependency.

> Note: the original design scoped Python as a follow-up; the shipped
> implementation includes the Python extraction path as well.

### UI (Next.js, `ui/src`)

- **`lib/fileUpload.ts`** — `FILE_ACCEPT`, `MAX_FILE_BYTES` (10 MB), `isAllowedFile`,
  `fileToFilePart` (read file → base64 `FilePart`). Allowlist: images, PDF,
  text/markdown, CSV, JSON.
- **`components/chat/ChatInterface.tsx`** — attach button + hidden multi-file
  `<input>`; `pendingFiles` state with removable chips; type/size validation with
  toasts; build `FilePart`s and append to the outgoing message; session naming
  falls back to the first file name for file-only messages.
- **`components/chat/FileAttachment.tsx`** (new) — image thumbnail (object URL)
  or a download chip (icon, filename, human size, download button).
- **`components/chat/ChatMessage.tsx`** — render file parts in user and agent
  bubbles.
- **`lib/messageHandlers.ts`** — preserve `file` parts from messages and from
  `artifact-update` events (`extractMessagesFromTasks`).

### Edge / deployment

- **`helm/kagent/files/nginx.conf`** — add
  `client_max_body_size {{ .Values.ui.nginx.clientMaxBodySize }};`.
- **`helm/kagent/values.yaml`** — `ui.nginx.clientMaxBodySize: 50m` (default).
- **`helm/kagent/tests/ui-nginx-configmap_test.yaml`** — assert the default and
  an override render into the config.

### Acceptance criteria (AC1–AC8)

AC1 artifact service wired · AC2 upload passed inline · AC3 upload persisted ·
AC4 agent-saved artifact emitted as A2A `FilePart` · AC5 `loadartifactstool`
registered · AC6 UI multi-file attach with validation · AC7 thumbnail/chip
rendering in both bubbles · AC8 E2E upload round trip on kind.

### Test Plan

- **Go unit:** `adapter_test.go` (non-nil `ArtifactService`), `agent_test.go`
  (tool list includes load/save tools), `executor_test.go` (ArtifactDelta →
  emitted `FilePart`; oversized inbound → failed status), `artifacts_test.go`,
  `save_artifact_tool_test.go`, `fileextract` tests (`fileextract_test.go`,
  `fixture_test.go`), `models/openai_adk_test.go`.
- **Go e2e:** `go/core/test/e2e/file_upload_test.go` — upload an inline A2A file
  part to a Go ADK agent and assert it is processed (uses the current
  `a2aproject/a2a-go/v2` API).
- **Python unit:** `tests/unittests/models/test_file_extract.py`,
  `test_openai.py`.
- **UI unit:** `lib/__tests__/fileUpload.test.ts`,
  `lib/__tests__/messageHandlers.test.ts`,
  `chat/__tests__/FileAttachment.test.tsx`, `chat/__tests__/ChatMessage.test.tsx`.
- **Helm:** `helm/kagent/tests/ui-nginx-configmap_test.yaml`.

## Alternatives

- **Core-server artifact endpoints / mounting `adkrest`:** more moving parts, a
  second transport, and `adkrest` has no upload route — rejected in favor of
  A2A-only.
- **Diff the artifact store per turn:** racy and fragile versus the idiomatic
  `ArtifactDelta` event signal.
- **Inline-only download (no store surfacing):** wouldn't connect the artifact
  store to the UI.
- **Send raw files to every model:** token bloat and many models can't read
  PDFs — hence server-side text extraction with the 10 MB cap.

## Open Questions

- Durable/shared artifact storage for multi-replica deployments and restarts.
- Per-file size limit configurability beyond the current 10 MB UI cap +
  `client_max_body_size`.
- Whether to expose an artifact browser (list/version/delete) in the UI.
