# Implementation Priority

## P1: PathValidator Hardening

Our policy layer must guard against workspace escapes, so contributors need the
correct API references when implementing fixes. The examples below mirror the
actual `policy.PathValidator` surface so there is no confusion when following
this plan.

### Example Usage
```go
workspace := "/work/project"
additionalRoots := []string{"/tmp/shared"}
validator, err := policy.NewPathValidator(workspace, additionalRoots)
if err != nil {
    return fmt.Errorf("configure validator: %w", err)
}

cleanPath, err := validator.ValidatePath(userInput)
if err != nil {
    return fmt.Errorf("reject path: %w", err)
}
// safe to use cleanPath: it is absolute and confined to the workspace or allowed roots
```

`NewPathValidator(workspace, allowedRoots)` always returns a validator that
normalizes the workspace root and any additional allowed directories. The
validator exposes two helpers:

- `ValidatePath(string) (string, error)` — returns an absolute safe path.
- `Workspace() string` — exposes the canonical workspace root for diagnostics.

### Files to Change
- `internal/policy/policy.go`
- `internal/policy/pathvalidator_test.go`

Keep tests focused on `pathvalidator_test.go`; the older
`policy_test.go` file no longer exists. This ensures reviewers look in the
correct location when verifying the hardening work.
