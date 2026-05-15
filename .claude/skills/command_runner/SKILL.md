# Command Runner

## Rule

**ALWAYS use mage commands instead of raw go/bash commands.**

If a mage command doesn't exist for what you need:
1. Ask the user if they want to add it
2. If accepted, add it to `magefile.go`
3. Then execute and continue with original task

## Available Commands

### Build

```bash
# Build the binary
mage build
```

### Test

```bash
# Run all unit tests (default)
mage test

# Run tests with verbose output
mage test:verbose

# Run all integration tests
mage test:integration

# Run specific integration tests (by pattern)
mage test:integration TestStreamCompletion
```

### Clean

```bash
# Remove build artifacts
mage clean
```

## Command Reference

| Task | Mage Command | NOT This |
|------|--------------|----------|
| Build binary | `mage build` | `go build ./cmd/...` |
| Run all tests | `mage test` | `go test ./...` |
| Run tests verbose | `mage test:verbose` | `go test -v ./...` |
| Integration tests | `mage test:integration` | `go test ./integration/...` |
| Specific integration | `mage test:integration TestStream` | `go test -run TestStream ./integration/...` |
| Clean artifacts | `mage clean` | `rm -rf ...` |

## Missing Commands

If you need a command not listed above, **ask first**. Common candidates:

- `mage test:coverage` - Run tests with coverage report
- `mage test:package <pkg>` - Run tests for specific package
- `mage lint` - Run linter
- `mage fmt` - Format code
- `mage run <args>` - Build and run with arguments
