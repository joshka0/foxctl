package protocol

// ErrorCode represents a canonical error code from Core Profile v1.
type ErrorCode string

const (
	// ErrorCodeEARG indicates invalid arguments or validation errors.
	ErrorCodeEARG ErrorCode = "EARG"
	// ErrorCodeEOpenAPI indicates OpenAPI spec parsing or validation errors.
	ErrorCodeEOpenAPI ErrorCode = "EOPENAPI"
	// ErrorCodeEAuth indicates authentication or credential problems.
	ErrorCodeEAuth ErrorCode = "EAUTH"
	// ErrorCodeEPagination indicates pagination-related failures.
	ErrorCodeEPagination ErrorCode = "EPAGINATION"
	// ErrorCodeERateLimit indicates rate limit or backoff exhaustion.
	ErrorCodeERateLimit ErrorCode = "ERATELIMIT"
	// ErrorCodeERuntime indicates runtime or generic execution failure (HTTP 5xx, runner crash, etc.).
	ErrorCodeERuntime ErrorCode = "ERUNTIME"
	// ErrorCodeERuntimeRestart indicates the runner restarted but recovered.
	ErrorCodeERuntimeRestart ErrorCode = "ERUNTIME_RESTART"
	// ErrorCodeEOutputTooLarge indicates output exceeded capture limits.
	ErrorCodeEOutputTooLarge ErrorCode = "EOUTPUT_TOO_LARGE"
	// ErrorCodeEPolicy indicates capability or policy violations.
	ErrorCodeEPolicy ErrorCode = "EPOLICY"
	// ErrorCodeENotFound indicates resource not found.
	ErrorCodeENotFound ErrorCode = "ENOTFOUND"
	// ErrorCodeETimeout indicates operation timeout.
	ErrorCodeETimeout ErrorCode = "ETIMEOUT"
	// ErrorCodeESkillDown indicates skill unavailability.
	ErrorCodeESkillDown ErrorCode = "ESKILLDOWN"
	// ErrorCodeEParse indicates JSON parse or invalid UTF-8 errors.
	ErrorCodeEParse ErrorCode = "EPARSE"
	// ErrorCodeEEnvelope indicates invalid or malformed envelope.
	ErrorCodeEEnvelope ErrorCode = "EENVELOPE"
	// ErrorCodeEIO indicates filesystem or I/O errors.
	ErrorCodeEIO ErrorCode = "EIO"
	// ErrorCodeECanceled indicates job was canceled by user.
	ErrorCodeECanceled ErrorCode = "ECANCELED"
)

// String returns the string representation of the error code.
func (c ErrorCode) String() string {
	return string(c)
}

// IsRetryable returns true if the error code indicates a transient failure
// that may succeed on retry.
func IsRetryable(code ErrorCode) bool {
	switch code {
	case ErrorCodeERuntime, ErrorCodeERateLimit, ErrorCodeETimeout:
		return true
	default:
		return false
	}
}
