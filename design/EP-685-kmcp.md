<!--
**Note:** When your Enhancement Proposal (EP) is complete, all of these comment blocks should be removed.

This template is inspired by the Kubernetes Enhancement Proposal (KEP) template: https://github.com/kubernetes/enhancements/blob/master/keps/sig-architecture/0000-kep-process/README.md

To get started with this template:

- [ ] **Create an issue in kagent-dev/kagent**
- [ ] **Make a copy of this template.**
  `EP-[ID]: [Feature/Enhancement Name]`, where `ID` is the issue number (with no
  leading-zero padding) assigned to your enhancement above.
- [ ] **Fill out this file as best you can.**
  At minimum, you should fill in the "Summary" and "Motivation" sections.
- [ ] **Create a PR for this EP.**
  Assign it to maintainers with relevant context.
- [ ] **Merge early and iterate.**
  Avoid getting hung up on specific details and instead aim to get the goals of
  the EP clarified and merged quickly. The best way to do this is to just
  start with the high-level sections and fill out details incrementally in
  subsequent PRs.

Just because a EP is merged does not mean it is complete or approved. Any EP
marked as `provisional` is a working document and subject to change. You can
denote sections that are under active debate as follows:

```
<<[UNRESOLVED optional short context or usernames ]>>
Stuff that is being argued.
<<[/UNRESOLVED]>>
```

When editing EPS, aim for tightly-scoped, single-topic PRs to keep discussions
focused. If you disagree with what is already in a document, open a new PR
with suggested changes.

One EP corresponds to one "feature" or "enhancement" for its whole lifecycle. Once a feature has become
"implemented", major changes should get new EPs.
-->
# EP-685: First class support for kmcp

<!--
This is the title of your EP. Keep it short, simple, and descriptive. A good
title can help communicate what the EP is and should be considered as part of
any review.
-->

* Issue: [#685](https://github.com/kagent-dev/kagent/issues/685)

## Background 

<!-- 
Provide a brief overview of the feature/enhancement, including relevant background information, origin, and sponsors. 
Highlight the primary purpose and how it fits within the broader ecosystem.

Include Motivation, concise overview of goals, challenges, and trade-offs.

-->

As the issue states, the goal of this EP is to provide first class support for kmcp. For background, kmcp is a project which provides a standard way to build tools, as well as run run them inside of kubernetes. This was a feature that has been missing in kagent for a while, so it's important to get this right.

The main goal of this EP is to provide a way to use kmcp `MCPServer` resources as a tool server in kagent. However, there are a number of smaller goals which will be required to achieve this.



## Motivation


<!--
This section is for explicitly listing the motivation, goals, and non-goals of
this EP. Describe why the change is important and the benefits to users. The
motivation section can optionally provide links to [experience reports] to
demonstrate the interest in a EP within the wider Kubernetes community.

[experience reports]: https://github.com/golang/go/wiki/ExperienceReports
-->

Currently the only way to access tools in kagent is via the ToolServer CRD. However, now that kmcp has been released, the UX is not that great. If I want to use kmcp, I need to first create the MCPServer resource, and then create a ToolServer resource to point to it. I should be able to just use the `MCPServer` resource directly.


### Goals

<!--

List the specific goals of the EP. What is it trying to achieve? How will we
know that this has succeeded?

Include specific, actionable outcomes. Ensure that the goals focus on the scope of
the proposed feature.
-->

As stated above, the main goal of this EP is to provide a way to use kmcp `MCPServer` resources as a tool server in kagent.

To accomplish this, we will need to break the current `Agent` CRD, which will require bumping the version from `v1alpha1` to `v1alpha2`. This will allow us to add a new field to the `Agent` CRD, which will be used to specify the tool server to use.

Because we are breaking this API, we have the oppurtunity to make some other changes to the API which are related, and we also view as important. For example, we view `kmcp` as the best way to run `stdio` MCP servers going forward, and therefore there is no need to support `ToolServer` resources as they exist today.

Therefore the goal is to instead create a new `RemoteMCPServer` CRD, which will be used specifically when a remote MCP server is needed. In addition, we can allow the new version of the `Agent` CRD to specify a `Service` resource to use as the MCP server, thereby skipping custom resources altogether for this use-case.


### Non-Goals 

<!--
What is out of scope for this EP? Listing non-goals helps to focus discussion
and make progress.
-->

## Implementation Details

<!--
This section should contain enough information that the specifics of your
change are understandable. This may include API specs (though not always
required) or even code snippets. If there's any ambiguity about HOW your
proposal will be implemented, this is the place to discuss them.

-->

The core change of this EP will be the API changes. As a result of those, both the controller, CLI, and UI will need to be updated to support the new API.

Let's start with the Agent.

Currently the `Agent` CRD defines tools as follows:
```
    - type: McpServer
      mcpServer:
        toolServer: kagent-querydoc
        toolNames:
          - query_documentation
```

The proposal is to change this to the following:

```
    - type: McpServer
      mcpServer:
        name: kagent-querydoc
        kind: MCPServer
        group: kagent.dev
        toolNames:
          - query_documentation

```

Notice that in the first version we always require the tool to be a toolserver, whereas now we allow for any MCP server, with more potentially being added in the future. Another example of this with a service would look like the following:
```
    - type: McpServer
      mcpServer:
        name: kagent-querydoc
        kind: Service
        toolNames:
          - query_documentation
```

This would require no extra work on the user side, they would not need to create any new resources. In the case that they want `kagent` to discover the tools for selection in the UI, they will simply need to add a label to the service.

```
  labels:
    kagent.dev/mcp-server: "true"
```

Finally let's take a look at the `RemoteMCPServer` CRD. The core of the design would be a flattening of the current `ToolServer` CRD, while removing the stdio server. Below is an example of the current `ToolServer` CRD:

```
apiVersion: kagent.dev/v1alpha1
kind: ToolServer
metadata:
  name: {{ include "querydoc.fullname" . }}
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "querydoc.labels" . | nindent 4 }}
spec:
  config:
    streamableHttp:
      timeout: 30s
      url: {{ include "querydoc.serverUrl" . }}
  description: "Documentation Query Tool Server"


```

The new `RemoteMCPServer` CRD would look like the following:
```
apiVersion: kagent.dev/v1alpha2
kind: RemoteMCPServer
metadata:
  name: my-remote-mcp-server
  namespace: kagent
spec:
  protocol: <"SSE" | "STREAMABLE_HTTP">
  timeout: 30s
  url: "https://my-remote-mcp-server.example.com/mcp"
  description: "My Remote MCP Server"
```

The `protocol` field is used to specify the protocol to use to connect to the remote MCP server. By default it will choose `STREAMABLE_HTTP` if not specified.


### Test Plan 

<!--
    Define the testing strategy for the feature.
    Include unit, integration, and end-to-end (e2e) tests.
    Specify any additional frameworks or tools required for testing.
-->

## Alternatives

<!--
Highlight potential challenges or trade-offs.
-->

One major alternative which was considered is what I'll call the URL proposal. The URL proposal would have removed the tool-centric CRD completely, and instead opted to use a URL field to specify the MCP server to use.

```
    - type: McpServer
      mcpServer:
        url: "https://my-remote-mcp-server.example.com/mcp"
        toolNames:
          - query_documentation
```

There were a couple of issues with this approach that we decided to not pursue.

1. This would have made the UX of the UI much more complex. The UI would not be able to discover the tools for selection in the UI, and would instead need to rely on the user to manually enter the URL unless an agent already existed for that server. 
2. All authentication and connection information would have to be specific in the agent itself, which mean it could not be shared between agents.
3. The RBAC for the 2 use-cases would be combined. This would not allow organizations to have different RBAC rules for the 2 use-cases.


## Open Questions

<!--
Include any unresolved questions or areas requiring feedback.
-->