# Fork Session Design

## Goal

Add a **Fork** option alongside **Rewind** so the user can "duplicate chat from this point": create a **new** chat session whose history is the conversation **rewound to the selected message**, without modifying the current session. The user can then continue in the new chat (e.g. edit the input and send) as usual.

- **Rewind** (existing): Rewind the **current** session to before a given invocation; same session, truncated history.
- **Fork**: Create a **new** session whose history is the conversation up to (and including) the selected point; original session unchanged; user is taken to the new chat.

## User flow

1. User sees a message and opens the three-dots menu (already present on user messages with `rewindBeforeInvocationId`).
2. Menu options: **Rewind to here** | **Fork chat from here**.
3. **Rewind to here**: Current behavior (same session, rewind via A2A rewind request, UI truncates and refetches).
4. **Fork chat from here**: Backend creates a new session, copies effective session state and task history to the new session ID, returns the new session; frontend navigates to the new chat URL. User can then type and send in the new chat as usual.

## Current state (already implemented in UI)

- **ChatMessage**: Dropdown with "Rewind to here" and "Fork chat from here"; `onRewindToMessage` and `onForkFromMessage` props.
- **ChatInterface**: `handleRewindToMessage` (calls A2A rewind); `handleForkFromMessage` (calls `forkSession(sessionId, rewindBeforeInvocationId)` and then `router.push` to new session URL and dispatches `new-session-created`).
- **sessions.ts**: `forkSession` is **imported** by ChatInterface but **not yet implemented** (missing in `app/actions/sessions.ts`).

So the frontend shell is ready; we need the **backend fork API** and the **frontend `forkSession`** implementation.

## Backend: what “fork” must do

Fork is a **pure data operation** in the Go backend: no Python ADK or Runner involved. The backend must:

1. **Create a new session** (new ID, same `user_id`, same `agent_id`, name e.g. `"Fork of …"`).
2. **Copy effective session events** from the source session into the new session, up to (and not including) the first event whose `invocation_id` equals `rewind_before_invocation_id`. Events are stored in `Event` table with `SessionID`; copy = new rows with same `Data` and `UserID`, but `SessionID` = new session ID and new `ID` for each event (or keep event IDs for traceability; see below).
3. **Build effective task history** for the new session using the same boundary as rewind: list tasks for source session, find the first task that contains a message with `metadata.kagent_invocation_id === rewind_before_invocation_id`, keep only tasks before that, merge their histories into one effective history (same logic as Python `rewind_to_invocation`).
4. **Create one new task** for the new session with that effective history (new `task_id`, `context_id` = new session ID), and persist it (POST `/api/tasks` or equivalent DB write).
5. **Return the new session** (id, name, etc.) so the frontend can navigate to `/agents/{ns}/{agent}/chat/{newSessionId}`.

### Event copy semantics

- **Source**: `ListEventsForSession(sourceSessionID, userID)`. Backend returns events in **DESC** by `created_at`; for fork we need **chronological order** (oldest first), so reverse the list (or use ASC when adding a fork-specific list helper).
- **Boundary**: Parse each event’s `Data` (JSON). ADK Event has `invocation_id`. Find the **first** event (in chronological order) whose `invocation_id` equals `rewind_before_invocation_id`. All events **before** that (strictly before) are “effective”; that boundary event and all after are dropped.
- **Copy**: For each effective event, insert a new `Event` row: `SessionID` = new session ID, `UserID` = same, `Data` = same (or deep copy), `ID` = new UUID (so the new session has its own event IDs). This keeps the new session’s event log consistent and avoids ID collisions.

### Task history semantics (align with Python rewind_to_invocation)

- **Source**: `ListTasksForSession(sourceSessionID)`.
- **Boundary**: For each task, inspect `task.History` (or the parsed task’s history). Find the first task index `boundary_idx` such that some message in `task.History` has `metadata["kagent_invocation_id"] == rewind_before_invocation_id`. Then:
  - `tasks_to_keep` = `tasks[0:boundary_idx]`
  - Effective history = merge of all messages from `tasks_to_keep` (in order), deduplicating by `message_id`, and updating each message’s `context_id` (and optionally `task_id`) to the new session/task.
- **New task**: Build one `Task` with `id` = new UUID, `context_id` = new session ID, `history` = effective history, `status` = completed; then `StoreTask` (or POST `/api/tasks`).

### API shape (Go)

- **Endpoint**: `POST /api/sessions/{session_id}/fork` (or `POST /api/sessions/fork` with body `{ "session_id": "...", "rewind_before_invocation_id": "..." }`).
- **Auth**: Same as other session APIs (user_id from header/query).
- **Request body**: `{ "rewind_before_invocation_id": "..." }` (session_id from path).
- **Response**: Same as create session, e.g. `{ "data": { "id": "<new_session_id>", "name": "...", ... } }` so the UI can navigate and show the new session in the sidebar.

### Database / client (Go)

- **No new interface methods strictly required** if we can:
  - Create session: existing `StoreSession`.
  - List events: existing `ListEventsForSession` (then filter by parsing `Data` for `invocation_id` and take events before boundary).
  - Store events: existing `StoreEvents` (batch insert with new session ID and new event IDs).
  - List tasks: existing `ListTasksForSession`.
  - Store task: existing `StoreTask` (for the single new task with effective history).
- **Optional**: Add a helper that returns events in chronological order for a session to avoid reversing in the handler.

## Frontend: what to implement

1. **`forkSession(sessionId, rewindBeforeInvocationId)`** in `app/actions/sessions.ts`:
   - Call `POST /api/sessions/{sessionId}/fork` (or the chosen path) with body `{ rewind_before_invocation_id: rewindBeforeInvocationId }`.
   - Use existing `fetchApi` and `getCurrentUserId()`; pass `user_id` as for other session APIs.
   - Return `Promise<BaseResponse<Session>>`; on success, `data` is the new session (with `id`, `name`, etc.).
2. **ChatInterface** already:
   - Calls `forkSession(currentSessionId, rewindBeforeInvocationId)` in `handleForkFromMessage`.
   - On success, navigates to `router.push(newUrl)` and dispatches `new-session-created` with the new session. No change needed there once `forkSession` is implemented.

## Python / ADK

- **No changes.** Fork is implemented entirely in the Go backend and frontend. The Python executor and task store are only used when the user **continues** the forked chat (send message); the first load of the forked chat is from the Go session/task APIs (tasks for the new session ID), and the first new message will create a normal A2A request for that new session.

## Summary of code changes

| Layer        | Change |
|-------------|--------|
| **Go**      | New handler: `POST /api/sessions/{session_id}/fork` with body `rewind_before_invocation_id`. Create new session; copy events up to boundary (parse Event.Data for `invocation_id`); build effective task history from source tasks, create one new task for new session; return new session. Register route in server. |
| **Go DB**   | Use existing `StoreSession`, `ListEventsForSession`, `StoreEvents`, `ListTasksForSession`, `StoreTask`. Optionally add a small helper to parse invocation_id from event JSON. |
| **UI**      | Implement `forkSession(sessionId, rewindBeforeInvocationId)` in `app/actions/sessions.ts` calling the new fork API; return type `BaseResponse<Session>`. |
| **Python**  | None. |

## Edge cases

- **Missing boundary**: If no event has `invocation_id === rewind_before_invocation_id`, treat as “fork from start” (copy all events; effective task history = all tasks’ histories merged) or return 400 with a clear message. Prefer “fork from start” for robustness.
- **Empty effective history**: If boundary is the first event (no events before), new session has no events and one task with empty history; UI shows “Start a conversation” and user can type. Acceptable.
- **Concurrent rewind**: Fork reads source session/tasks at request time; if the source is rewound in parallel, fork may still copy the pre-rewind state. No strong consistency required for v1; fork is a snapshot.

## Invocation ID and event JSON (Go)

- **Events**: Stored as `Event.Data` (text). Content is ADK Event JSON from Python (`event.model_dump_json()`). The ADK Event has a top-level field `invocation_id` (string). When listing events for fork, parse `Data` as JSON and read `invocation_id` to find the boundary.
- **Tasks**: Stored as `Task.Data` (text) or equivalent; when parsing protocol.Task, history is a list of messages; each message can have `metadata["kagent_invocation_id"]`. Use the same boundary logic as Python `KAgentTaskStore.rewind_to_invocation`.

## See also

- [Rewind flow](rewind-flow.md) — How rewind works and how task store is updated (Option B').
- [A2A tasks](a2a-tasks.md) — Task detection and storage.
