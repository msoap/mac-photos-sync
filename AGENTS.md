# Repository Guidelines

## Project Structure & Module Organization

`main.go` provides the `mac-photos-sync` CLI. Implementation packages live under `internal/`: `photosdb` reads the Photos database, `model` holds shared types, `metadata` handles file metadata, `sync` plans and runs synchronization, and `catalog` and `filesystem` manage destination state and files. Keep package tests beside their source in `*_test.go` files. `docs/` contains design notes; `plans/` records project plans. `test_data/from/` is a sample Photos library for investigation, while `test_data/to/` is a generated destination mirror. Do not commit generated output.

## Build, Test, and Development Commands

- `make build`: build the local `mac-photos-sync` binary.
- `make build-amd64`: build `mac-photos-sync-amd64` for Intel Macs.
- `make install`: install the CLI with `go install .`.
- `make test` or `go test ./...`: run all Go tests.
- `./mac-photos-sync -library "/path/to/Photos Library.photoslibrary" -dry-run /path/to/mirror`: inspect a sync without applying changes.

Run these commands from the repository root. Use `./mac-photos-sync -help` for current CLI options.

## Coding Style & Naming Conventions

Format Go code with `gofmt` and follow standard Go package and identifier conventions. Use tabs as produced by `gofmt`. Prefer short, descriptive variable names; use one-letter names only in short loops, functions of at most three lines, or a tiny scope where the value is used once nearby. Always call the binary `mac-photos-sync` in code, commands, and documentation.

## Testing Guidelines

Use Go's `testing` package and name test files `*_test.go` and test functions `Test...`. Cover database interpretation, sync planning, and file operations with focused fixtures or temporary directories. Run `go test ./...` before submitting a change. There is no documented coverage threshold.

## Commits & Pull Requests

Recent commits use brief subjects such as `Added select` and `Updated README.md`; write a concise subject that states the change. In pull requests, summarize behavior, list tests run, and link a related issue when one exists. For sync behavior changes, include a representative dry-run result and describe effects on the source library and destination files.

## Source Library Safety

Treat the Photos library as read-only. Close Photos.app before running a sync, and use `-dry-run` when checking a new library or destination. Keep any test that changes files confined to temporary directories or the generated destination, never `test_data/from/`.
