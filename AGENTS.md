# Sanctify - Agent Guide

Sanctify is an automated versioning and release tool built with Go.

## Development Conventions (Critical)

- **Code**: The project is a single file `main.go`. DO NOT split into multiple files.
- **Messages**: Use strict `Conventional Commits` v1.0.0.
- **Errors**: Return `error` from functions; `main()` handles `os.Exit`.
- **Git**: Use `go-git` for all operations.
- **Robustness**: Support execution outside of Git repositories (use default `0.0.0` version).

## Validation Commands (Mandatory)

**CRITICAL: You MUST execute ALL of these commands after ANY code changes to ensure technical integrity.**

- **Tests**: `go test -v ./...`
- **Build**: `go build -o /dev/null main.go`
- **Lint**: `go vet ./...`
- **Format**: `go fmt ./...`
- **Modules**: `go mod tidy`

## Project Structure

- `main.go`: Logic for `parseCommit`, `calculateNextVersion`, and `updateVersionInFiles`.
- `main_test.go`: Core logic tests using `t.TempDir()`.
- `VERSION`: Plain text file with current version.

## Version Update Targets

Agents must ensure all these functions/files are updated when adding new format support:
- `updateJSONVersion`: `package.json`, `package-lock.json`, `composer.json`, `metadata.json`.
- `updateYAMLVersion`: `meta/main.yml`, `*.info.yml`.
- `updateHCLVersion`: `version.tf`.
- `updateDockerVersion`: `Dockerfile*`.
- `updateGoVersion`: `main.go` (`const Version`).
- `updatePlainVersion`: `VERSION`.
