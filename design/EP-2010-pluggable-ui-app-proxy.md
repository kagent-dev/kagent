# EP-2010: Pluggable UI App Proxy

* Status: **Superseded** by [EP-2004](EP-2004-dynamic-mcp-ui-routing.md)
* Spec: [specs/pluggable-ui-app-proxy](../specs/pluggable-ui-app-proxy/)

## Background

Early concept for making plugin UIs accessible via `/plugins/<name>/` URLs through an API proxy. This rough idea was superseded by the more comprehensive dynamic MCP UI routing system (EP-2004).

## Motivation

Plugin UIs needed to be accessible within the kagent shell without hardcoded nginx rules.

### Goals

- Extend existing API and UI proxy
- Make plugin UI accessible via `/plugins/<name>/` URLs

### Non-Goals

- This EP was superseded before detailed goals were defined

## Implementation Details

Superseded by EP-2004 which provides CRD-driven routing, iframe bridge, and plugin discovery.
