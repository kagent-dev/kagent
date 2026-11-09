---
name: architecture-flow-docs
description: Write or update a kagent architecture page that explains an end-to-end flow, such as creating an Agent or sending a message, with an Excalidraw diagram of the components involved. Use when documenting a flow in docs/architecture or when an architecture change makes such a page out of date.
---

# Architecture flow docs

A flow page explains one end-to-end operation: which components take part, what
each is responsible for, what kagent stores, and why the parts interact as they
do. Readers are contributors deciding where a change belongs. Example:
[agent-creation.md](../../../docs/architecture/agent-creation.md).

## Principles

- Describe responsibilities and reasons, not calls. Name what a component does
  and does not do; leave RPC, method, and query names to the code links.
- Verify every claim in the code and schema before writing it: services,
  handlers, SQL migrations, and constraints. Plausible guesses are often wrong.
  For example, "the Session service has no Actor client" was false (it creates
  and suspends Actors through ate-api), and "reservations are keyed by the
  message" was false (Session creation and saved tasks are).
- Draw one diagram per set of responsibilities. When a flow has variants, such
  as a new or existing conversation, show the differences in a table instead of
  a second diagram.
- Describe failures in prose and say which component owns each retry or cleanup.
- Keep the text short and plain. Do not use Mermaid or the phrase "happy path".

## Page structure

1. Introduction: two sentences saying what the flow does, ending with "Use it to
   decide where a change belongs."
2. The diagram.
3. Phases: at most eight, one sentence each, numbered to match the diagram
   arrows. Add a variants table when needed.
4. Responsibilities: a table of Component | Responsible for | Not responsible for.
5. What kagent stores: a table of Record | Holds | Why. Give each record a plain
   name followed by its table names in backticks, such as
   "Session (`runtime_instance`, `session`)".
   Follow it with a short list of how the records relate.
6. Why it works this way: the design reasons behind the boundaries.
7. Where changes belong: common changes and the component that owns each.
8. Failures and recovery.
9. Code: one link per component.

Link the page from [docs/architecture/README.md](../../../docs/architecture/README.md)
and from related pages.

## Diagram layout

- Lanes: User Interaction (top row or left column), Kubernetes, Application,
  and Database & storage.
- Groups: kagent controller and Substrate. Each box has a name and a one-line
  responsibility.
- Arrows carry a phase number and a short purpose.
- Legend: green for SQL, blue for gRPC between kagent and Substrate, black for
  Kubernetes API or A2A, grey dashed for work inside a component.
- Grey bands behind arrows mark connections held open, such as a stream.
- Database boxes use plain record names, linked by dashed arrows labeled with
  the relation, such as "pins" or "belongs to".
- Add the note "Substrate's own state is not shown."

## Scene format

The `.excalidraw` scene is the source. Generate it from a throwaway script in
`.plans/` rather than drawing by hand, so layout changes stay cheap. Do not
commit the script. Copy element shapes from the existing scenes in
`docs/architecture/images/`.

- Colors: text and Kubernetes or A2A arrows `#1e1e1e`, SQL `#2f9e44`, gRPC
  `#1971c2`, internal work `#868e96`, notes `#495057`.
- Group fills: kagent controller `#f3f0ff`, Substrate `#fff4e6`, storage
  `#fff9db`.
- Every element uses `fontFamily: 2` and `roughness: 0`. Boxes use
  `roundness: {type: 3}`. Box labels are text elements bound with `containerId`.
- The renderer draws text at its stored position and does not re-center it, so
  the script must compute it: width is about characters × font size × 0.56, and
  line height is 1.25 × font size.
- Font sizes: 28 for lane titles, 20 for group titles, 15–16 for box labels, and
  14–16 for arrow labels.

## Workflow

1. Trace the flow through the code and schema.
2. Draft the page text and share it for review before drawing.
3. Write the generator in `.plans/` and output the scene to
   `docs/architecture/images/NAME.excalidraw`.
4. Render it as described under "Diagram changes" in the
   [kagent-dev skill](../kagent-dev/SKILL.md). A scene that is not yet tracked
   needs `DIAGRAM_SCENES=docs/architecture/images/NAME.excalidraw`. Preview with
   `rsvg-convert -w 1800 NAME.svg -o NAME.png`, then fix overlapping labels and
   arrows and render again.
5. Commit the page, scene, and SVG together, then run `make diagrams-check`.
6. Open one pull request per page. Stack related pages with `gh stack`.
