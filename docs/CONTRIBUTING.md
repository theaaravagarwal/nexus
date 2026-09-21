# Contributing

Thanks for contributing to `nexus`.

## Development Setup

Prerequisites:

- Go 1.26.3+
- `ssh`
- `rsync`
- `fzf`

Install deps and run checks:

```bash
make check
```

## Branch and PR Workflow

1. Create a branch from `main`.
2. Keep changes focused and include tests for behavior changes.
3. Open a PR and wait for CI, lint, and vulnerability checks to pass.
4. Squash-merge unless maintainers request otherwise.

## Code Guidelines

- Keep CLI behavior stable and backward compatible when possible.
- Return actionable errors; avoid panic paths for user input.
- Preserve script-friendly output (`--help`, exit codes, predictable messages).
- Add tests for parsing, host persistence, config, and transfer behavior changes.

## Commit Guidelines

Use short imperative commit subjects, for example:

- `fix host validation for bracketed IPv6`
- `add tests for config bootstrap`

## Release Process

Releases are automated with GoReleaser from Git tags:

1. Update `CHANGELOG.md`.
2. Tag a version: `git tag vX.Y.Z`.
3. Push tag: `git push origin vX.Y.Z`.
4. GitHub Actions publishes artifacts and checksums.

## Required Repository Settings

Set these in GitHub repository settings:

- Protect `main` branch.
- Require PRs before merge.
- Require status checks: CI, lint, dependency scan.
- Require at least one approving review.
