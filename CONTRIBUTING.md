# Contribution Guidelines

- [Ways to contribute](#ways-to-contribute)
  - [Report Security Vulnerabilities](#report-security-vulnerabilities)
  - [File issues](#file-issues)
  - [Find something to work on](#find-something-to-work-on)
- [Community Assignments](#community-assignments)
  - [Assignment Process](#assignment-process)
  - [Stale Assignment Policy](#stale-assignment-policy)
  - [Best Practices for Assignees](#best-practices-for-assignees)
- [Contributing code](#contributing-code)
  - [Small changes (bug fixes)](#small-changes-bug-fixes)
  - [Large changes (features, refactors)](#large-changes-features-refactors)
  - [Tips to get started](#tips-to-get-started)
- [Requirements for PRs](#requirements-for-prs)
  - [DCO](#dco)
  - [Testing](#testing)
    - [Unit Tests](#unit-tests)
    - [End-to-End (E2E) Tests](#end-to-end-e2e-tests)
  - [Code review guidelines](#code-review-guidelines)
- [Documentation](#documentation)
- [Get in touch](#get-in-touch)

## Ways to contribute

Thanks for your interest in contributing to kagent! We have a few different ways you can get involved. To understand contributor roles, refer to the [contributor ladder guide](https://github.com/kagent-dev/community/blob/main/CONTRIBUTOR_LADDER.md).

### Report Security Vulnerabilities

If you would like to report a security issue, please refer to our [SECURITY.md](SECURITY.md) file.

### File issues

To file a bug or feature request in the [kagent GitHub repo](https://github.com/kagent-dev/kagent):

1. Search existing issues first.
2. If no existing issue addresses your case, create a new one.
3. Use [issue templates](https://github.com/kagent-dev/kagent/tree/main/.github/ISSUE_TEMPLATE) when available
4. Add information or react to existing issues, such as a thumbs-up 👍 to indicate agreement.

### Find something to work on

The project uses [GitHub issues](https://github.com/kagent-dev/kagent/issues) to track bugs and features. Issues labeled with the [`good first issue`](https://github.com/kagent-dev/kagent/issues?q=state%3Aopen%20label%3A%22good%20first%20issue%22) label are a great place to start.

Additionally, the project has a [project board](https://github.com/orgs/kagent-dev/projects/3) tracking the roadmap. Any issues in the project board are a great source of things to work on. If an issue has not been assigned, you can ask to work on it by leaving a comment on the issue.

Flaky tests are a common source of issues and a good place to start contributing to the project. You can find these issues by filtering with the `Type: CI Test Flake` label. If you see a test that is failing regularly, you can leave a comment asking if someone is working on it.

## Community Assignments

We welcome community contributions and encourage members to work on issues. To maintain an active and healthy development environment, we have the following policies:

### Assignment Process

- **Organization members**: Can self-assign issues using the GitHub assignee dropdown
- **External contributors**: Should comment on the issue expressing interest in working on it. A maintainer will then assign the issue to you.

### Stale Assignment Policy

- **Timeframe**: If an assignee hasn't made any visible progress (comments, commits, or draft PRs) within **30 days** of assignment, the issue assignment may be considered stale
- **Communication**: We'll reach out to check on progress and offer assistance before unassigning
- **Unassignment**: After **5 additional days** without response or progress, issues will be unassigned and made available for other contributors
- **Re-assignment**: Previous assignees are welcome to request re-assignment if they become available to work on the issue again

### Best Practices for Assignees

- Comment on the issue with your approach or ask questions if you need clarification
- Provide regular updates (even brief ones) if work is taking longer than expected
- Create draft PRs early to show progress and get feedback
- Don't hesitate to ask for help in the issue comments or community channels like Discord or CNCF Slack
- Join the community meetings to share progress or engage with other members for discussions

## Contributing code

Contributing features to kagent is a great way to get involved with the project. We welcome contributions of all sizes, from small bug fixes to large new features. Kagent uses a "fork and pull request" approach. This means that as a contributor, you create your own personal fork of a code repository in GitHub and push your contributions to a branch in your own fork first. When you are ready to contribute, open a pull request (PR) against the project's repository. For more details, see the [GitHub docs about working with forks](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/working-with-forks).

### Small changes (bug fixes)

For small changes (less than 100 lines of code):

1. Open a pull request.
2. Ensure tests verify the fix.
3. If needed, [update the documentation](#documentation).

### Large changes (features, refactors)

Large features often touch many files, extend many lines of code, and often cover issues such as:

* Large bug fixes
* New features
* Refactors of the existing codebase

For large changes:

1. **Open an issue first**: Open an issue about your bug or feature in the [kagent](https://github.com/kagent-dev/kagent) repo.
2. **Message us on Slack or Discord**: Reach out to us to discuss your proposed changes in our [CNCF Slack channel, `#kagent-dev`](https://cloud-native.slack.com/archives/C08ETST0076) or [Discord server](https://discord.gg/Fu3k65f2k3).
3. **Agree on implementation plan**: Write a plan for how this feature or bug fix should be implemented. Should this be one pull request or multiple incremental improvements? Who is going to do each part? Discuss it with us on Slack/Discord or join our [community meeting](https://calendar.google.com/calendar/u/0?cid=Y183OTI0OTdhNGU1N2NiNzVhNzE0Mjg0NWFkMzVkNTVmMTkxYTAwOWVhN2ZiN2E3ZTc5NDA5Yjk5NGJhOTRhMmVhQGdyb3VwLmNhbGVuZGFyLmdvb2dsZS5jb20).
4. **Submit a draft PR**: It's important to get feedback as early as possible to ensure that any big improvements end up being merged. Open a draft pull request from your fork, label it `work in progress`, and start getting feedback.
5. **Review**: At least one maintainer should sign off on the change before it's merged. Look at the following [Code review](#code-review-guidelines) section to learn about what we're looking for.
6. **Close out**: A maintainer will merge the PR and let you know about the next release plan.

For large or broad changes, we may ask you to write an enhancement proposal. Use [this template](https://github.com/kagent-dev/kagent/blob/main/design/template.md) to get you started. You can find the existing enhancement proposals [here](https://github.com/kagent-dev/kagent/tree/main/design).

### Tips to get started

To help you get started with contributing code:

- **Development Setup**: See the [DEVELOPMENT.md](DEVELOPMENT.md) file for detailed instructions on setting up your development environment.
- **Code of Conduct**: Please read and follow our [Code of Conduct](CODE_OF_CONDUCT.md).
- **Past PRs**: We recommend looking at past PRs that are doing similar things to what you are trying to do.
- **Agent Examples**: Check out the [sample agents](https://github.com/kagent-dev/kagent/tree/main/python/samples) for examples of how to build agents.
- **Architecture**: Review the [architecture documentation](https://github.com/kagent-dev/kagent#architecture) to understand how kagent works.

## Requirements for PRs

Contributing to open source can be a daunting task, especially if you are a new contributor and are not yet familiar with the workflows commonly used by open source projects.

After you open a PR, the project maintainers will review your changes. Reviews typically include iterations of suggestions and changes. This is totally normal, so don't be discouraged if asked to make changes to your contribution.

It's difficult to cover all the possible scenarios that you might encounter when contributing to open source software in a single document. However, this contributing guide outlines several requirements that even some well-versed contributors may not be familiar with. If you have questions, concerns or just need help getting started please don't hesitate to reach out through one of the channels covered in the [Get in touch section](#get-in-touch).

### DCO

DCO, short for Developer Certificate of Origin, is a per-commit signoff that you, the contributor, agree to the terms published at [https://developercertificate.org](https://developercertificate.org) for that particular commit. This will appear as a `Signed-off-by: Your Name <your.email>` trailer at the end of each commit message. The kagent project requires that every commit contains this DCO signoff.

The easiest way to make sure each of your commits contains the signoff is to run make `init-git-hooks` in the repo to which you are contributing. This will configure your repo to use a Git hook which will automatically add the required trailer to all of your commit messages.

```shell
make init-git-hooks
```

If you prefer not to use a Git hook, you must remember to use the `--signoff` option (or `-s` for short) on each of your commits when you check in code:

```shell
git commit -s -m "description of my excellent contribution"
```

If you forget to sign off on a commit, your PR will be flagged and blocked from merging. You can sign off on previous commits by using the rebase command. The following example uses the `main` branch, which means this command rewrites the `git` history of your current branch while adding signoffs to commits visible from `main` (not inclusive). Please be aware that rewriting commit history does carry some risk, and if the commits you are rewriting are already pushed to a remote, you will need to force push the rewritten history.

```shell
git rebase --signoff main
```

### Testing

Tests are essential for any non-trivial PR. They ensure that your feature remains operational and does not break due to future updates. Tests are a critical part of maintaining kagent's stability and long-term maintainability.

A useful way to explore the different tests that the project maintains, is to inspect the [GitHub action that runs the CI pipeline](.github/workflows/ci.yaml)

We have the following types of tests:

#### Unit Tests

These are useful for testing small, isolated units of code, such as a single function or a small component.

**Go Unit Tests**:

```bash
cd go
go test -race -skip 'TestE2E.*' -v ./...
```

**Helm Unit Tests**:

```bash
helm plugin install https://github.com/helm-unittest/helm-unittest
make helm-version
helm unittest helm/kagent
```

**Python Unit Tests**:

   ```bash
cd python
uv run pytest ./packages/**/tests/
   ```

**UI Unit Tests**:

   ```bash
cd ui
npm run test
```

#### End-to-End (E2E) Tests

These tests are done in a `kind` cluster with real agents, using real or mock LLM providers.  
See: [go/test/e2e](https://github.com/kagent-dev/kagent/tree/main/go/test/e2e)

Features that introduce behavior changes should be covered by E2E tests (exceptions can be made for minor changes). Testing with real Kubernetes resources and agent invocations is crucial because it:

- Prevents regressions.
- Detects behavior changes from dependencies.
- Ensures the feature is not deprecated.
- Confirms the feature works as the user expects it to.

### Code review guidelines

Code can be reviewed by anyone! Even if you are not a maintainer, please feel free to add your comments.
All code must be reviewed by at least one [maintainer](https://github.com/kagent-dev/community/blob/main/MAINTAINERS.md) before merging. Key requirements:

1. **Code Style**

   **Go Code**:
   - Follow [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments).
   - Follow [Effective Go](https://golang.org/doc/effective_go).
   - Run `make lint` to check for common issues before submitting.

   **Python Code**:
   - Follow PEP 8 style guidelines.
   - Run `make lint` to check for common issues before submitting.

   **UI Code**:
   - Follow the project's ESLint configuration.
   - Run `npm run lint` before submitting.

2. **Testing**

   - Add unit tests for new functionality.
   - Ensure existing tests pass.
   - Include e2e tests when needed.

3. **Documentation**

   - Update relevant documentation.
   - Include code comments for non-obvious logic.
   - Update API documentation if changing interfaces.
   - Add examples for new features.

## Documentation

The kagent documentation lives at [kagent.dev/docs](https://kagent.dev/docs/kagent). The code lives at [kagent website](https://github.com/kagent-dev/website).

## Get in touch

Please refer to the [Project README](README.md#get-involved) for methods to get in touch
