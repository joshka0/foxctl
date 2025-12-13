package runservice

import (
	"encoding/json"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
)

func annotateCorrelationAndJob(result []byte, jobID, correlationID string) []byte {
	if len(result) == 0 {
		return result
	}
	var envObj envelope.Envelope
	if err := json.Unmarshal(result, &envObj); err != nil {
		return result
	}
	if jobID != "" {
		envObj.Meta.JobID = jobID
	}
	if correlationID != "" {
		envObj.Meta.CorrelID = correlationID
	}
	out, err := json.Marshal(envObj)
	if err != nil {
		return result
	}
	return out
}
