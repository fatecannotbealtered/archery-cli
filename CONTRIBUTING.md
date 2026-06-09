# Contributing to archery-cli

Thank you for your interest in contributing to archery-cli.

## Development Setup

### Prerequisites

- Go version declared in `go.mod`
- Git
- golangci-lint (for linting)

### Getting Started

```bash
# Clone the repository
git clone https://github.com/fatecannotbealtered/archery-cli.git
cd archery-cli

# Build the binary
make build

# Run tests
go test -race ./...

# Run linter
golangci-lint run
```

## Branch Strategy

- Create feature branches from `main`: `git checkout -b feat/your-feature`
- Keep branches focused on a single change
- Open a Pull Request (PR) targeting `main` when ready
- Rebase or merge latest `main` before requesting review

## Commit Format

This project uses [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>: <description>

<optional body>
```

| Type | Use for |
|------|---------|
| `feat` | New features |
| `fix` | Bug fixes |
| `refactor` | Code restructuring without behavior change |
| `docs` | Documentation changes |
| `test` | Adding or updating tests |
| `chore` | Build, CI, dependency, or tooling changes |
| `perf` | Performance improvements |
| `ci` | CI/CD pipeline changes |

Examples:

```
feat: add slow query export command
fix: handle nil pointer in workflow status check
docs: update README install instructions
```

## Functional contract coverage

Release standard: **Functional Contract Coverage = 100%**. Every public behavior documented in README, Skill, `archery-cli reference`, `--help`, `context`, `doctor`, `changelog`, or `update` must have automated command-level tests.

For each new or changed command, cover success, invalid arguments, config/auth/permission failure where applicable, upstream failure or timeout where applicable, JSON envelope shape, output schema, exit code, stdout/stderr boundary, and non-interactive behavior. Every bug fix that changes observable behavior needs a regression test.

Numeric line coverage is tracked separately and may ratchet upward, but it does not replace missing contract tests.

## Pull Request Checklist

Before submitting a PR, ensure:

- [ ] All tests pass: `go test -race ./...`
- [ ] Functional Contract Coverage remains 100% for public behavior
- [ ] Code is formatted: `gofmt -s -w .`
- [ ] No vet warnings: `go vet ./...`
- [ ] npm dependencies are installed from the lockfile: `npm ci --ignore-scripts`
- [ ] npm dependency audit passes: `npm audit --audit-level=high`
- [ ] Linter passes: `golangci-lint run`
- [ ] Documentation is updated if behavior changed
- [ ] `CHANGELOG.md` is updated under `[Unreleased]`
- [ ] Commit messages follow the conventional commit format

## Code Style

- Use `gofmt` for formatting (enforced in CI)
- Run `go vet` to catch common issues
- Run `golangci-lint` for additional static analysis
- Follow existing patterns in the codebase
- Keep functions focused and small
- Handle errors explicitly; do not discard them with `_`

## Testing Requirements

```bash
# Run all tests with race detector
go test -race ./...

# Run tests with verbose output
go test -v -race ./...

# Run tests for a specific package
go test -race ./internal/api/...
```

- Write tests for new functionality
- Ensure existing tests still pass
- Aim for meaningful coverage, not just line count

## Reporting Issues

- Use GitHub Issues for bug reports and feature requests
- Include steps to reproduce for bugs
- Include your Go version (`go version`) and OS
