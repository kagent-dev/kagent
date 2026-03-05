# Existing Git/Repository References

## What Exists

### 1. GitHub MCP Server (`/contrib/tools/github-mcp-server/`)
- Helm chart wrapping GitHub's official MCP server
- GitHub API operations: PRs, Issues, Discussions, Actions, Repos metadata
- NOT git operations (no clone/pull/push)
- Token auth via K8s Secrets

### 2. UI Placeholder (`/ui/src/app/git/page.tsx`)
- Renders "GIT Repos — Coming soon" with GitFork icon
- No functionality

### 3. This Spec (`/specs/git-repos-api-ui/`)
- Planning artifacts (this project)

## What Does NOT Exist
- No `GitRepository` CRD type
- No git clone/pull/push/branch functionality
- No git operation handlers in HTTP server
- No git utilities in ADK or agent runtime
- No agent secrets for git auth (SSH keys, PATs)
- No database models for git repositories

## Implication
Git Repos is a **greenfield feature** — needs CRD, controller, DB model, API handlers, and UI pages.
The GitHub MCP server is complementary (GitHub API ops) but separate from git repo management.
