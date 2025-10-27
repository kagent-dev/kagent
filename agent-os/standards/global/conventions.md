## General development conventions

### Project Structure

**Multi-Language Monorepo:**
```
kagent/
├── go/                    # Controller, API server, CLI
│   ├── api/               # API definitions
│   ├── cli/               # CLI commands
│   ├── cmd/               # Main applications
│   ├── internal/          # Internal packages
│   │   ├── controller/    # Kubernetes controller
│   │   ├── database/      # Database layer
│   │   └── httpserver/    # HTTP API handlers
│   ├── pkg/               # Public packages
│   └── test/              # Tests
├── python/                # Agent runtimes
│   └── packages/          # Python packages
│       ├── kagent-adk/    # ADK integration
│       ├── kagent-langgraph/  # LangGraph integration
│       ├── kagent-crewai/ # CrewAI integration
│       └── kagent-core/   # Core utilities
├── ui/                    # Next.js web UI
│   └── src/
│       ├── app/           # Next.js App Router
│       ├── components/    # React components
│       └── lib/           # Utilities
└── agent-os/              # Standards and skills
    └── standards/         # Coding standards
```

### Version Control Best Practices

#### Commit Messages

Follow **Conventional Commits** format:

```
<type>(<scope>): <subject>

<body>

<footer>
```

**Types:**
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation only
- `style`: Code style (formatting, no logic change)
- `refactor`: Code refactoring
- `test`: Adding or updating tests
- `chore`: Maintenance (dependencies, build, etc.)

**Examples:**
```
feat(ui): add agent creation wizard

fix(api): handle 404 errors in agent endpoint

docs(readme): update installation instructions
```

#### Branching Strategy

- **main**: Production-ready code
- **feature/{name}**: Feature branches
- **fix/{name}**: Bug fix branches

**Workflow:**
1. Create feature branch from `main`
2. Make changes and commit
3. Push and create Pull Request
4. Review and merge to `main`

#### Pull Request Process

- Write clear PR description
- Reference related issues
- Ensure CI passes
- Request code review
- Address review comments
- Squash and merge (or rebase)

### Environment Configuration

#### Go Environment Variables

```bash
DATABASE_URL=postgresql://localhost/kagent
SQLITE_PATH=./kagent.db
PORT=8080
LOG_LEVEL=info
```

#### Python Environment Variables

```bash
KAGENT_URL=http://localhost:8080
STS_WELL_KNOWN_URI=https://sts.example.com
LOG_LEVEL=INFO
```

#### Frontend Environment Variables

```bash
NEXT_PUBLIC_API_URL=http://localhost:8080
NEXT_PUBLIC_USER_ID=admin@kagent.dev
```

**Important:**
- Never commit secrets to version control
- Use `.env` files for local development
- Use `.env.example` as template (without secrets)
- Configure secrets via environment in production

### Dependency Management

#### Go (go.mod)

```bash
# Add dependency
go get github.com/some/package

# Update dependencies
go get -u ./...

# Tidy modules
go mod tidy
```

#### Python (uv)

```bash
# Install dependencies
uv pip install -e .

# Add dependency (edit pyproject.toml)
# Then sync
uv pip install -e .

# Update dependencies
uv pip install --upgrade-package <package>
```

#### Node.js (npm)

```bash
# Install dependencies
npm install

# Add dependency
npm install <package>

# Update dependencies
npm update
```

**Best Practices:**
- Keep dependencies minimal
- Document why major dependencies are used
- Regularly update dependencies (security)
- Pin versions for reproducibility

### Code Review Process

#### As Author

1. **Self-Review**: Review your own changes first
2. **Small PRs**: Keep changes focused and small
3. **Clear Description**: Explain what and why
4. **Tests**: Include tests for new features/fixes
5. **CI Green**: Ensure all checks pass
6. **Respond Promptly**: Address reviewer feedback quickly

#### As Reviewer

1. **Be Constructive**: Suggest improvements, don't just criticize
2. **Check Logic**: Verify correctness and edge cases
3. **Readability**: Ensure code is clear and maintainable
4. **Tests**: Verify adequate test coverage
5. **Standards**: Check adherence to project conventions
6. **Approve or Request Changes**: Be clear about blocking issues

### Testing Requirements

**Before Merging:**
- Unit tests pass
- Integration tests pass (if applicable)
- E2E tests pass (for UI changes)
- Linting passes
- No regression in existing functionality

**Test Coverage:**
- Critical paths: Required
- Edge cases: Recommended
- Error handling: Required for key flows
- UI components: Major features only

### Feature Flags

For incomplete or experimental features:

```go
// Go feature flag
if viper.GetBool("feature.new_agent_ui") {
    // New implementation
} else {
    // Old implementation
}
```

```python
# Python feature flag
if os.getenv("FEATURE_NEW_AGENT_UI") == "true":
    # New implementation
else:
    # Old implementation
```

**When to Use:**
- Gradual rollouts
- A/B testing
- Incomplete features
- Risky changes

### Changelog Maintenance

Keep track of changes in git commits and releases:

**Release Notes Format:**
```markdown
## v0.5.0 (2025-01-27)

### Features
- Added agent creation wizard (#123)
- Support for Anthropic Claude models (#125)

### Bug Fixes
- Fixed session persistence issue (#127)
- Resolved chat scrolling on mobile (#128)

### Breaking Changes
- Changed API endpoint from /agents to /api/agents
```

### Documentation

#### Code Documentation

- **README.md**: Project overview, setup, usage
- **DEVELOPMENT.md**: Local development setup
- **CONTRIBUTION.md**: How to contribute
- **API Documentation**: OpenAPI/Swagger specs
- **Architecture Docs**: High-level system design

#### When to Update Docs

- New features added
- API changes
- Breaking changes
- Configuration changes
- Deployment process changes

### File Organization

**Go Packages:**
- One package per directory
- Package name matches directory name
- `internal/` for private packages
- `pkg/` for public packages

**Python Packages:**
- One package per directory in `packages/`
- Private modules use leading underscore: `_a2a.py`
- Public API in `__init__.py`

**TypeScript/React:**
- Component files use PascalCase: `ChatInterface.tsx`
- Utility files use kebab-case: `utils.ts`
- Feature-based organization
- Shared UI components in `components/ui/`

### Naming Conventions Summary

| Language | Files | Functions | Classes | Constants |
|----------|-------|-----------|---------|-----------|
| Go | lowercase | PascalCase/camelCase | PascalCase | PascalCase/ALL_CAPS |
| Python | snake_case | snake_case | PascalCase | ALL_CAPS |
| TypeScript | PascalCase/camelCase | camelCase | PascalCase | ALL_CAPS |
| JSON | kebab-case | - | - | - |
| SQL | snake_case | snake_case | - | UPPER |

### Security Best Practices

1. **Never Commit Secrets**: Use environment variables
2. **Validate All Input**: Server-side validation required
3. **Parameterized Queries**: Prevent SQL injection
4. **HTTPS Only**: In production
5. **Authentication**: Verify user identity
6. **Authorization**: Check permissions
7. **Rate Limiting**: Prevent abuse
8. **Error Messages**: Don't expose internal details
9. **Logging**: Don't log sensitive data
10. **Dependencies**: Keep updated for security patches

### Performance Considerations

- **Database**: Index frequently queried columns
- **API**: Implement pagination for large datasets
- **Frontend**: Lazy load images and components
- **Caching**: Cache expensive computations
- **N+1 Queries**: Use eager loading
- **Bundle Size**: Tree-shake unused code

### Accessibility Requirements

- **WCAG AA Compliance**: Minimum standard
- **Keyboard Navigation**: Full keyboard support
- **Screen Readers**: ARIA labels and semantic HTML
- **Color Contrast**: 4.5:1 for text
- **Focus Indicators**: Visible focus states

### CI/CD Pipeline

From `.github/workflows/ci.yaml`:

**On PR:**
- Linting
- Unit tests
- Build verification

**On Merge to Main:**
- Full test suite
- Build Docker images
- Tag release (if version bumped)

### Monitoring and Observability

- **Prometheus**: Metrics collection
- **OpenTelemetry**: Distributed tracing
- **Structured Logging**: JSON format for easy parsing
- **Error Tracking**: Log errors with context
