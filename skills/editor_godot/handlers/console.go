package handlers

import "fmt"

// ConsoleHandler handles console actions: console_output.
type ConsoleHandler struct{}

func init() {
	h := &ConsoleHandler{}
	Register(ActionConsoleOutput, h)
}

func (h *ConsoleHandler) Validate(in Input) error {
	// No required fields for console_output
	return nil
}

func (h *ConsoleHandler) BuildParams(in Input) map[string]any {
	params := make(map[string]any)
	if in.MaxResults > 0 {
		params["limit"] = in.MaxResults
	}
	return params
}

func (h *ConsoleHandler) GenerateSummary(action string, data any) string {
	m, _ := data.(map[string]any)

	if m != nil {
		if messages, ok := m["messages"].([]any); ok {
			return fmt.Sprintf("Retrieved %d console message(s)", len(messages))
		}
	}
	return "Retrieved console output"
}
