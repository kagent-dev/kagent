# Requirements Q&A

<!-- Questions and answers will be appended here as requirements clarification progresses -->

## UI Requirements

- The Kanban board UI must display a distinct icon for each workflow stage so stages are recognizable at a glance.
- Stage-to-icon mapping should be consistent across column headers, card status badges, and any stage picker UI.
- Minimum stage coverage for icon mapping: `Inbox`, `Plan`, `Develop`, `Testing`, `CodeReview`, `Release`, and `Done`.
- Stage icons should be visually distinct, accessible in dark mode, and include text labels (icons must not be the only status indicator).
- Use a single icon set across the app (for example Lucide) to keep visual style consistent.

## Next.js Guidelines for UI

- Build with Next.js App Router conventions: server components by default, and client components only where interactivity is required.
- Keep TypeScript strict typing enabled and avoid `any` for stage/status types; define a shared status enum/union for board state.
- Use `next/image` for static image assets and optimize images for responsive display.
- Keep board layout and reusable stage/card UI in composable components under `ui/src/components`.
- Preserve accessibility: semantic headings for columns, keyboard focus states, and sufficient color contrast for icons and badges.

## New Requirements — Attachments & Labels (2026-02-25)

### Labels (confirmed)
- Labels already designed in v1.1 (R13, AC-12): free-form strings, case-insensitive filtering, label chips in UI.

### Attachments
- Tasks can have zero or more text file attachments (markdown, diff, plain text) and/or external links.
- Attachment types: markdown files (DESIGN.md, COMMENTS.md), diff files (CHANGES.diff), links to agent sessions, external web URLs.

**Q1:** Storage approach — should attachment file content be stored in the database (TEXT column) or on disk with a reference path in the DB?
**A1:** Database TEXT column. Keep it simple, no disk/volume needed.

**Q2:** Should attachments have distinct types, or a single model that covers both?
**A2:** Single Attachment model with a `type` field: `file` (filename + content TEXT) or `link` (url + optional title). Shared fields: id, task_id, created_at, updated_at.

**Q3:** MCP tools for attachments — what operations should agents have?
**A3:** Only `add_attachment` and `delete_attachment`. No list/get/update — keep it minimal. Attachments are returned inline when fetching a task via `get_task` or `get_board`.

**Q4:** UI rendering — how should attachments appear on task cards?
**A4:** Card view: paperclip icon + attachment count. Detail view (click on card): show full attachment list — filenames for files, clickable URLs for links. Markdown files rendered inline, diffs shown as code blocks.

**Q5:** Should `delete_task` cascade to attachments too (like it does for subtasks)?
**A5:** Yes. Deleting a task deletes all its attachments (same cascade pattern as subtasks).

**Q6:** Can subtasks also have attachments, or only top-level tasks?
**A6:** Only top-level tasks. Subtasks are more like checklists — no attachments.
