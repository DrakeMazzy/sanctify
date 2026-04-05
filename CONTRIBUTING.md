# Contributing to Sanctify

Thanks for your interest in contributing! Here's how to get started.

## Development Setup

```bash
git clone https://github.com/DrakeMazzy/sanctify.git
cd sanctify
go mod download
go test -v ./...
```

## Making Changes

1. Fork the repository and create a feature branch
2. Make your changes in `main.go` (single-file architecture — do not split)
3. Write or update tests in `main_test.go`
4. Run all checks:

```bash
go test -v ./...
go build -o /dev/null main.go
go vet ./...
go fmt ./...
go mod tidy
```

## Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/) v1.0.0:

- `feat: add new feature` — triggers minor version bump
- `fix: resolve issue` — triggers patch version bump
- `feat!: breaking change` or footer `BREAKING CHANGE:` — triggers major version bump
- `docs:`, `test:`, `ci:`, `chore:` — maintenance, no version bump

## Pull Requests

- Keep PRs focused on a single change
- Ensure all checks pass before requesting review
- Update `README.md` if your change affects usage

## License

By contributing, you agree that your contributions will be licensed under CC0 1.0 Universal.
