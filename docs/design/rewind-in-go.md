# Migrating Rewind to the Go Backend

## Summary

**Yes, it makes sense** to move rewind into the Go backend. The Python Runner’s rewind logic only:

1. Reads session + events from the KAgent API (Go).
2. Computes `state_delta` and `artifact_delta` from those events.
3. Builds a rewind event and appends it via the same API.
4. Task pruning is already done via the Go task store from Python.

So rewind is “mess with session, events, and tasks” using data that already lives in Go. The only part that’s Python-specific is the computation of deltas and the call into the artifact service; the rest can be done in Go by modifying the DB (and calling the same APIs) from new routes.

## What Python rewind does today

1. **Session + events**  
   `session_service.get_session()` → GET `/api/sessions/{id}` (Go). Session and events are already in the Go DB.

2. **Find boundary**  
   First event where `event.invocation_id == rewind_before_invocation_id`. Pure logic on event list.

3. **State delta**  
   - Replay `actions.state_delta` from `events[0:rewind_event_index]` → `state_at_rewind_point` (skip keys starting with `app:` or `user:`; `v is None` → pop key).
   - `current_state` = session state after replaying **all** events (in Python this is done in memory when building the session).
   - `rewind_state_delta`: (a) for each key in `state_at_rewind_point`, if `current_state[key] != value_at_rewind` then set `rewind_state_delta[key] = value_at_rewind`; (b) for each key in `current_state` (excluding `app:`/`user:`), if key not in `state_at_rewind_point` then `rewind_state_delta[key] = None`.

4. **Artifact delta**  
   Uses `artifact_service` (InMemoryArtifactService in KAgent) to restore artifact versions. Involves reading/writing artifact blobs.

5. **Append rewind event**  
   Builds an ADK `Event` with `rewind_before_invocation_id`, `state_delta`, `artifact_delta` and calls `session_service.append_event()` → POST `/api/sessions/{id}/events` (Go).

6. **Tasks**  
   Executor calls `task_store.rewind_to_invocation()` (Go API: list tasks, delete from boundary, return effective history) and then `task_store.save(task)` (Go). So task rewind is already “modify DB in Go.”

## What can be done in Go

| Piece | In Go? | Notes |
|-------|--------|--------|
| Session + events | ✅ Already in Go | List events for session, chronological order. |
| Find boundary | ✅ | Parse `Event.Data` JSON for `invocation_id`. |
| State delta | ✅ | Parse each event’s `Data` for `actions.state_delta`; replay for `events[0:boundary]` → `state_at_rewind_point`; replay for all events → `current_state`; then same diff logic as Python. |
| Rewind event (with state_delta) | ✅ | Build ADK-shaped rewind event JSON and `StoreEvents(rewind_event)`. |
| Task rewind | ✅ | Same as fork: list tasks, find boundary, delete from boundary, build one task with effective history, `StoreTask`. |
| Artifact delta | ⚠️ No (today) | KAgent uses `InMemoryArtifactService`; artifacts are not in Go. Go rewind would set `artifact_delta: {}`. If you later persist artifacts in Go, you could implement artifact rewind in Go too. |

So: **session, events, and tasks can all be handled in Go.** State rewind is just replaying `state_delta` from event JSON; no Python needed. Only artifact version restore is tied to Python today.

## Proposed Go rewind API

- **Endpoint**: `POST /api/sessions/{session_id}/rewind`
- **Body**: `{ "rewind_before_invocation_id": "..." }`
- **Auth**: Same as other session APIs (e.g. user_id from header/query).

**Handler steps:**

1. Load session and list events (chronological); validate user/agent.
2. Find boundary index: first event whose `Data` (parsed as JSON) has `invocation_id == rewind_before_invocation_id`. If not found, return 400.
3. **State delta (Go)**  
   - Parse each event’s `Data` as generic JSON; read `actions.state_delta` (map).  
   - Replay for `events[0:boundary]`: for each key (skip `app:`/`user:`), apply value or remove if `null` → `state_at_rewind_point`.  
   - Replay for **all** events → `current_state`.  
   - Build `rewind_state_delta` with the same rules as Python (align keys with `state_at_rewind_point`, remove keys only in `current_state` via `null`).
4. **Artifact delta**  
   Set `artifact_delta = {}` (no artifact service in Go today).
5. **Rewind event**  
   Build one new event JSON compatible with ADK:
   - `invocation_id`: new UUID
   - `author`: `"user"`
   - `actions`: `rewind_before_invocation_id`, `state_delta`, `artifact_delta`
   (Use the same field names/snake_case as ADK `Event.model_dump_json()` so that when the Runner loads the session it sees a valid rewind event.)
6. **Persist**  
   `StoreEvents(rewind_event)` with new event ID, same `session_id` / `user_id`.
7. **Task rewind**  
   Same as fork: list tasks for session, find boundary task (first task whose history contains a message with `metadata.kagent_invocation_id == rewind_before_invocation_id`), keep tasks before boundary, merge histories (dedupe by message_id), build one new task with effective history and `context_id = session_id`, `StoreTask` (and optionally delete old tasks from boundary onward if you want a single “current” task per session).
8. Return 200 or 201 with a simple success payload (e.g. `{ "message": "Rewound", "data": null }` or session snapshot).

## Benefits of doing rewind in Go

- **Single place for rewind**: UI and any other client call Go only; no need to hit the Python agent for a “control” rewind.
- **No Python for rewind-only**: You don’t need to spin up the Runner just to rewind; only when the user sends a new message after rewind.
- **Consistency**: Same DB and logic for fork and rewind (events + tasks); fewer moving parts.
- **Easier to reason about**: Session/events/tasks are the source of truth in Go; rewind is just “append this event + fix tasks.”

## Caveats

1. **Artifacts**  
   With `artifact_delta = {}`, rewind in Go does **not** restore artifact versions. If the app rarely uses artifacts or you’re okay with “state and conversation rewind only,” this is acceptable. If you need artifact rewind, either keep a rewind path through Python for that case or later add artifact storage + restore in Go.

2. **ADK event shape**  
   The rewind event JSON stored by Go must match what the ADK expects when it loads the session (e.g. when the user sends the next message). That means matching the exact field names and structure (e.g. `actions.state_delta`, `actions.artifact_delta`, `actions.rewind_before_invocation_id`). One way to be sure is to capture a sample rewind event from Python (`event.model_dump_json()`) and mirror that in Go.

3. **Rewind + invoke in one request**  
   Today the UI can send “rewind and then run this message” in one A2A request; the executor does rewind then invoke. If rewind moves to Go, the UI would:
   - Call `POST /api/sessions/{id}/rewind` with `rewind_before_invocation_id`, then
   - Call the normal A2A message/stream with the new user message (same session).
   So one extra HTTP round-trip for “rewind then send” unless you add a combined “rewind and return” flow that the UI uses before sending the message.

## Recommendation

- **Implement rewind in Go** for session (rewind event + state_delta) and tasks, with `artifact_delta = {}` and document that artifact versions are not restored when rewind is done via Go.
- **UI**: For “Rewind to here,” call `POST /api/sessions/{session_id}/rewind` instead of the A2A rewind control message; then refresh tasks (and optionally replace local messages with task history). For “rewind and send,” call rewind first, then send the message on the same session.
- **Python**: Keep the Runner’s `rewind_async` for now so that any code path that still goes through Python (e.g. if you later want artifact rewind) continues to work; you can deprecate or remove the “rewind via A2A” path once the Go rewind route is the only one used by the UI.

This keeps rewind logic where the data lives (Go) and only leaves artifact handling as a future improvement (in Go or via Python).
