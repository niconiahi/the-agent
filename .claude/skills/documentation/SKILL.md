# Documentation Skill

## When to Use

After completing a milestone or major feature:
1. Update `CHANGELOG.md` with all notable additions
2. Create integration tests for the feature's full pipeline

## What to Document

For each completed milestone, document:

- **Packages**: New packages created
- **Functions**: Public and notable private functions
- **Methods**: Struct methods
- **Constants**: Important constants with their values
- **Types**: Structs, interfaces, type aliases
- **Errors**: Exported error variables

## Format

```markdown
## Milestone: Title

Brief description of what this milestone adds to the system.

### Package: `internal/package_name`

**Types**:
- `TypeName` - brief description

**Constants**:
- `CONSTANT_NAME = value` - what it's for

**Functions**:
- `FunctionName(params) returns` - what it does

**Methods**:
- `(receiver) MethodName(params) returns` - what it does

**Errors**:
- `ERROR_NAME` - when it occurs
```

## Integration Tests

Milestones should have integration tests in `/integration` with **task-oriented naming**.

**Location**: `integration/<task_description>_test.go`

**Naming**: Describe the user task, not the feature:
- `stream_completion_test.go` (not `provider_test.go`)
- `agent_tool_execution_test.go` (not `agent_test.go`)

## Rules

1. Update CHANGELOG.md immediately after milestone completion
2. Create integration tests for the milestone's pipeline
3. Use code formatting for types, functions, constants
4. Keep descriptions brief (one line)
5. Group by category (Types, Constants, Functions, etc.)
6. Include file paths when helpful: `internal/ai/stream.go:15`
