# Rough Idea

Show running Temporal workflows in KAgent UI on the `/workflows` page.

Currently the `/workflows` page is a stub ("Coming soon"). The Temporal workflow executor has been designed (see `specs/temporal-agent-workflow/`) and partially implemented. The UI needs a native workflows page that shows running, completed, and failed Temporal workflows — giving users visibility into agent execution state without needing to access the raw Temporal UI.

This page should integrate with the existing kagent UI patterns (expandable rows, status indicators, actions) and fetch workflow data either from the Temporal server directly or via a new kagent backend API endpoint.
