package skillerr

// Standard error codes for skill errors.
// These codes are machine-readable and used in envelope.Error responses.
const (
	// CodeArg indicates an invalid argument or input (EARG).
	// Use for missing required fields, invalid values, or constraint violations.
	CodeArg = "EARG"

	// CodeRuntime indicates a runtime/execution error (ERUNTIME).
	// Use for unexpected failures during skill execution.
	CodeRuntime = "ERUNTIME"

	// CodeParse indicates a parsing error (EPARSE).
	// Use for JSON decode failures, YAML parse errors, etc.
	CodeParse = "EPARSE"

	// CodeIO indicates an I/O error (EIO).
	// Use for file read/write failures, network errors, etc.
	CodeIO = "EIO"

	// CodeAuth indicates an authentication/authorization error (EAUTH).
	// Use for missing API keys, invalid credentials, permission denied, etc.
	CodeAuth = "EAUTH"

	// CodeValidation indicates a validation error (EVALIDATION).
	// Use for schema validation, business rule violations, etc.
	CodeValidation = "EVALIDATION"

	// CodeNotFound indicates a resource not found error (ENOTFOUND).
	// Use for missing files, records, or external resources.
	CodeNotFound = "ENOTFOUND"

	// CodeTimeout indicates a timeout error (ETIMEOUT).
	// Use for operation timeouts, context deadline exceeded, etc.
	CodeTimeout = "ETIMEOUT"

	// CodeConflict indicates a conflict error (ECONFLICT).
	// Use for concurrent modification, version conflicts, etc.
	CodeConflict = "ECONFLICT"

	// CodeInternal indicates an internal error (EINTERNAL).
	// Use for unexpected internal failures that shouldn't happen.
	CodeInternal = "EINTERNAL"

	// CodeRuntimeRestart indicates a recoverable runtime error requiring restart (ERUNTIME_RESTART).
	// Use when the skill needs to be restarted (e.g., daemon connection lost).
	CodeRuntimeRestart = "ERUNTIME_RESTART"

	// CodeCapability indicates a capability error (ECAPABILITY).
	// Use for unsupported features, missing capabilities, or platform limitations.
	CodeCapability = "ECAPABILITY"

	// CodeIntegration indicates an external integration error (EINTEGRATION).
	// Use for external service failures, API errors, or third-party integration issues.
	CodeIntegration = "EINTEGRATION"
)

// CodeDescription returns a human-readable description of an error code.
func CodeDescription(code string) string {
	switch code {
	case CodeArg:
		return "Invalid argument or input"
	case CodeRuntime:
		return "Runtime execution error"
	case CodeParse:
		return "Parsing error"
	case CodeIO:
		return "I/O error"
	case CodeAuth:
		return "Authentication or authorization error"
	case CodeValidation:
		return "Validation error"
	case CodeNotFound:
		return "Resource not found"
	case CodeTimeout:
		return "Operation timed out"
	case CodeConflict:
		return "Conflict error"
	case CodeInternal:
		return "Internal error"
	case CodeRuntimeRestart:
		return "Recoverable runtime error requiring restart"
	case CodeCapability:
		return "Capability or feature not supported"
	case CodeIntegration:
		return "External integration error"
	default:
		return "Unknown error"
	}
}
