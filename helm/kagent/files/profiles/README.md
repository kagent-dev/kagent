# KAgent Profiles

KAgent's profiles provide a simpler way to set up KAgent in a configured way based on user needs.

Currently, there are two profiles:
1. `Demo`: For an installation of kagent that includes all our agents. This is useful for demo purposes and new users.
2. `Minimal`: (default) For an installation that does not include any pre-defined agent. This is useful for users who want to start from scratch.

**Important**: When adding a new profile or updating a name, we must add it to the list of profiles in our CLI's installation file so that they are also installable through the CLI. [ref](../../../../go/cli/internal/cli/install.go).
