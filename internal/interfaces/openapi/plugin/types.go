package plugin

import "encoding/json"

const (
	// CommandAuth is the command identifier for auth plugins.
	CommandAuth = "plugin/auth"
	// CommandPagination is the command identifier for pagination plugins.
	CommandPagination = "plugin/pagination"
)

// Limits describes the runtime limits communicated to plugins.
type Limits struct {
	// Wall is the wall clock timeout enforced by the parent.
	Wall int `json:"wall_ms,omitempty"`
	// CPU is the CPU budget enforced by the parent.
	CPU int `json:"cpu_ms,omitempty"`
	// MaxOutputKB caps stdout size in kilobytes.
	MaxOutputKB int `json:"max_out_kb,omitempty"`
	// MaxInputKB caps stdin size in kilobytes.
	MaxInputKB int `json:"max_in_kb,omitempty"`
}

// HTTPRequest represents the HTTP request snapshot shared with auth plugins.
type HTTPRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// HTTPResponse represents the HTTP response snapshot shared with pagination plugins.
type HTTPResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// AuthContext provides contextual information for auth plugins.
type AuthContext struct {
	SecurityScheme map[string]any `json:"security_scheme,omitempty"`
	Credentials    map[string]any `json:"credentials,omitempty"`
	SpecHints      map[string]any `json:"spec_hints,omitempty"`
}

// PaginationContext provides contextual information for pagination plugins.
type PaginationContext struct {
	PagingState map[string]any `json:"paging_state,omitempty"`
	SpecHints   map[string]any `json:"spec_hints,omitempty"`
}

// AuthRequestPayload is the payload forwarded to auth plugins.
type AuthRequestPayload struct {
	Request HTTPRequest `json:"request"`
	Context AuthContext `json:"context,omitempty"`
	Limits  Limits      `json:"limits,omitempty"`
}

// AuthResult is the response payload from auth plugins.
type AuthResult struct {
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// PaginationRequestPayload is the payload forwarded to pagination plugins.
type PaginationRequestPayload struct {
	LastResponse      HTTPResponse      `json:"last_response"`
	RequestedMaxItems int               `json:"requested_max_items,omitempty"`
	ItemsFetchedSoFar int               `json:"items_fetched_so_far,omitempty"`
	Context           PaginationContext `json:"context,omitempty"`
	Limits            Limits            `json:"limits,omitempty"`
}

// PaginationResult is the response payload from pagination plugins.
type PaginationResult struct {
	Continue    bool              `json:"continue"`
	NextURL     string            `json:"next_url,omitempty"`
	NextQuery   map[string]string `json:"next_query,omitempty"`
	NextCursor  string            `json:"next_cursor,omitempty"`
	ItemsInPage int               `json:"items_in_page,omitempty"`
}

// Handshake represents the metadata produced by the --handshake capability probe.
type Handshake struct {
	Name        string           `json:"name"`
	Version     string           `json:"version,omitempty"`
	Commands    []string         `json:"commands"`
	Protocols   []string         `json:"protocols"`
	Limits      *HandshakeLimits `json:"limits,omitempty"`
	Description string           `json:"description,omitempty"`
}

// HandshakeLimits describes optional plugin-reported limits.
type HandshakeLimits struct {
	MaxInKB  int `json:"max_in_kb,omitempty"`
	MaxOutKB int `json:"max_out_kb,omitempty"`
	CPUMs    int `json:"cpu_ms,omitempty"`
	WallMs   int `json:"wall_ms,omitempty"`
}
