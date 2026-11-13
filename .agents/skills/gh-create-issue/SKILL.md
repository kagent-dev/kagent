---
name: gh-create-issue
description: Prepare and open GitHub issues for bugs, feature requests, and design discussions.
---

# Create GitHub issues

Explain a bug or open a conversation about a need. Give readers enough context to understand the situation and why it matters, while leaving solutions and implementation decisions open. Cover bug reports, feature requests, and design discussions filed as GitHub issues.

Issues must not be prescriptive. Do not prescribe a fix, recommend an implementation, or add acceptance criteria, API designs, or task checklists. Investigation should make the problem clearer, not turn the issue into an implementation plan.

## Scope and authorization

- Before opening a GitHub issue, show the user the final title, body, target repository, and any labels, assignees, or other metadata to be applied. Wait for explicit confirmation to open that version. A general request to open an issue starts preparation; it does not confirm content the user has not yet reviewed.
- Honor confirmation already given for the same content and destination; do not ask again. If the content, destination, or metadata changes materially after confirmation, present the revised version and obtain confirmation before opening it.
- A request to investigate a problem, fix a bug, or explore a design does not authorize opening an issue.
- Never include enterprise issue or PR links, repository-qualified references, or identifying numbers in upstream issue titles, bodies, comments, or diagrams. Describe the problem independently.

## Ground the issue in the problem

Establish the intended repository and read its applicable instructions and issue templates. Use the user's context, relevant code and documentation, and existing issues or PRs to understand the behavior and existing conversations. Ask for missing information only when it materially changes the issue; continue useful investigation while waiting.

Investigate enough to support the claims the issue makes. Separate observed behavior, user-reported behavior, and suspected causes. A useful bug report does not require a completed fix or a confirmed root cause. Do not invent reproduction steps, affected versions, test results, user impact, or implementation commitments. Include uncertainty when it changes how readers should interpret or act on the issue; omit routine accounts of checks that could not be run.

When writing on Eitan's behalf, use `eitan-voice` if available. Keep the body direct and conversational: start with the actual need, explain the consequence, and give the reader enough context to participate. Prefer short paragraphs, with lists or headings where they make distinct points easier to follow. Follow required repository templates and title conventions without inventing answers or checking unsupported attestations. If a template requires a proposed solution, describe the desired user outcome and state that the approach is open for discussion. Otherwise avoid a fixed collection of empty sections.

Keep the issue focused on one coherent problem or question. Preserve important context in the main body; move bulky logs and supporting evidence into collapsed sections or link existing references. Do not publish additional comments or artifacts just to keep the body short unless that is authorized.

## Match the issue to the request

**Bug reports:** Describe the concrete trigger, expected behavior, actual behavior, and consequence. Include the smallest known reproduction and the environment or version details that affect it. If reproduction is incomplete or intermittent, say what is known. Add a short error or log excerpt when it helps identify the failure. Include evidence about a cause when it helps explain the bug, clearly distinguishing a hypothesis from a confirmed cause. Leave the fix open.

**Feature requests:** Start with the use case and current limitation, then describe what someone would like to be able to do and why. Use a concrete example when it clarifies the need. Explain why an existing workaround falls short when relevant. Open a conversation about that need without specifying how to implement it.

**Design discussions:** Introduce the situation or question worth exploring, explain why it matters, and give the context needed to discuss it. Surface relevant constraints and unresolved questions without selecting an approach or laying out a design. Invite input rather than presenting a decision for approval.

For all three, keep the title centered on the bug, need, or question. Invite discussion where it helps; a clear bug report does not need a forced closing question. Rewrite the title and body as understanding of the problem evolves.

## Explain interactions with collapsed Mermaid

Include a diagram when existing component interactions, data flow, or lifecycle behavior are central to understanding the issue. Use it to explain the situation, not design a solution. Omit it when prose or a small example already explains the problem.

- Show the smallest relevant set of components and label arrows with the operation or data they carry. Use a flowchart for relationships, a sequence diagram for ordering, or a state diagram for transitions.
- Ground the diagram in the available evidence and label uncertainty. Do not invent edges, ownership, or transaction boundaries, or introduce a proposed implementation through the diagram.
- Keep the problem, requested outcome, essential constraints, and open questions visible outside the collapsed block. The issue must make sense without opening the diagram.
- Wrap each diagram in `<details>` with a descriptive `<summary>` and no `open` attribute. Preserve blank lines around the unindented fenced `mermaid` block and before `</details>`. Use ordinary GitHub-supported Mermaid syntax, simple node IDs, and quoted flowchart labels containing punctuation.
- Check the syntax and the meaning of each edge. Use an available preview when useful; do not claim GitHub rendering was verified from a raw Markdown check or create a test issue to preview it.

## Prepare and open

When only drafting or preparing, return the proposed title and body or a local Markdown file. Continue refining that draft with the user; do not publish it.

When asked to open an issue:

1. Search for relevant open and closed issues and related PRs. If an existing issue already covers the request, return its URL and explain the overlap instead of creating a duplicate. Do not comment on, edit, or reopen it without authorization for that action. A similar issue may still leave a distinct gap; explain that difference when opening a new one.
2. Ensure the title and body reflect the agreed scope and available evidence. Use the applicable template's required content and repository-required metadata. Add assignees, milestones, projects, or other optional metadata only when requested.
3. Save the exact body as UTF-8 Markdown under `$TMPDIR`. Present the completed issue for confirmation as described above. Keep it local until the user confirms; silence or elapsed time is not confirmation.
4. Create only the confirmed version. Use a structured API body or `gh issue create --body-file` so backticks, newlines, and Mermaid fences survive unchanged. With `gh`, specify the repository and title explicitly.
5. If creation returns an ambiguous failure, check whether the issue was created before retrying. Stop and report an unresolved failure rather than repeatedly attempting mutations.
6. Read back the created issue and verify its repository, title, body, and requested metadata. Return its URL.
