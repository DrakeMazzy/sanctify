# Sanctify

Sanctify is a minimalist Go-based tool for automating versioning, changelog generation, and software releases. It automates tagging, updates version metadata in configuration files, and pushes changes to a remote Git repository.

## Features

1.  **Automated Versioning**: Calculates the next semantic version based on commit messages using strict **Conventional Commits v1.0.0** parsing (including Scopes, Breaking indicators `!`, and Footers).
2.  **Context-Aware Releases**: Supports different branch strategies:
    - `main`/`master` (or specified via `--tag-branch`) → stable version (e.g., `1.2.3`).
    - PR/Merge Requests → `-rc.N` suffix (e.g., `1.2.3-rc.42`).
    - Development branches → `-HASH` suffix (e.g., `1.2.3-abcdef1`).
3.  **Metadata Updates**: Automatically finds and updates versions in:
    - **Go**: `main.go` (updates `const Version = "..."`)
    - **Universal**: `VERSION` (plain text file)
    - **Node.js**: `package.json`, `package-lock.json`
    - **PHP/Composer**: `composer.json`
    - **Puppet**: `metadata.json`
    - **Ansible**: `meta/main.yml`
    - **Terraform**: `version.tf` (searches for `version = "..."` or `default = "..."`)
    - **Drupal**: `*.info.yml` (all files in the root)
    - **Docker**: `Dockerfile*` (labels and environment variables)
4.  **Changelog Generation**: Updates `CHANGELOG.md` with grouped commits (Breaking Changes, Features, Bug Fixes, Maintenance).
5.  **CI/CD Support**: Detects PR numbers from GitHub, GitLab, Bitbucket, and Jenkins.
6.  **Safety**: Displays help when run without parameters and works correctly outside of Git repositories (using default values).

## Installation

### 1. Direct Download (Recommended)
Download the latest pre-compiled binary for your platform from the [GitHub Releases](https://github.com/DrakeMazzy/sanctify/releases) page.

### 2. Using Go Install
```bash
go install github.com/DrakeMazzy/sanctify@latest
```

### 3. Manual Build
```bash
git clone https://github.com/DrakeMazzy/sanctify.git
cd sanctify
go build -o sanctify main.go
```

## Usage

### Running
```bash
# Help
sanctify --help

# Current version
sanctify --version

# Automated release
sanctify

# Dry run (prints version string only)
sanctify --dry-run

# Explicit context override
sanctify --context feature

# Custom tag branches
sanctify --tag-branch production,release

# Version override
sanctify 1.2.3
```

## CI/CD Examples

### GitHub Actions
```yaml
name: Auto Tag
on:
  push:
    branches: [main]
jobs:
  tag:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
        with:
          fetch-depth: 0
          token: ${{ secrets.GH_PAT }}
      - run: go run github.com/DrakeMazzy/sanctify@latest
        env:
          GITHUB_TOKEN: ${{ secrets.GH_PAT }}
```

### GitLab CI
```yaml
sanctify:
  stage: deploy
  image: golang:latest
  script:
    - go run github.com/DrakeMazzy/sanctify@latest
  only:
    - main
  variables:
    GITLAB_TOKEN: $GITLAB_RELEASE_TOKEN # Project Access Token with 'write_repository'
```

### Bitbucket Pipelines
```yaml
pipelines:
  branches:
    main:
      - step:
          name: Sanctify Release
          image: golang:latest
          script:
            - go run github.com/DrakeMazzy/sanctify@latest
          services:
            - docker
          variables:
            BITBUCKET_TOKEN: $BITBUCKET_ACCESS_TOKEN # Repository Access Token
```

## Architecture & Technologies

- **Language**: Go 1.26.1+
- **Libraries**: `go-git/v5`, `Masterminds/semver/v3`.
- **License**: **CC0 1.0 Universal** (Public Domain).
- **CI/CD**: GitHub Actions for testing and GoReleaser for automated releases.

## Development Conventions

- **Conventional Commits**: Follow the specification. Use `feat`, `fix`, `perf`, `docs`, `test`, `ci`, `chore`, `refactor`, `build`, `style`, `revert`.
- **Breaking Changes**: Use `!` in the header or `BREAKING CHANGE:` in the footer.
