# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Development

```bash
go build ./...          # Build all packages
go run .                # Run the main application
go test ./...           # Run all tests
go test ./pkg/foo       # Run tests for a specific package
go test -run TestName   # Run a single test by name
go vet ./...            # Static analysis
```

## Code Style

- Follow standard Go conventions (gofmt, go vet)
- Use `gofmt` or `goimports` for formatting
- Error handling: return errors rather than using panic; wrap errors with `fmt.Errorf("context: %w", err)`
- Naming: use MixedCaps/mixedCaps per Go conventions, not underscores
