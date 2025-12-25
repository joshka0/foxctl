package runservice

import "encoding/json"

// annotateCorrelationAndJob adds job_id and correlation_id to the envelope's meta
// without touching the data payload bytes.
func annotateCorrelationAndJob(result []byte, jobID, correlationID string) []byte {
	if len(result) == 0 {
		return result
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(result, &top); err != nil {
		return result
	}

	// Get or create meta map
	var meta map[string]any
	if rawMeta, ok := top["meta"]; ok && len(rawMeta) > 0 {
		if err := json.Unmarshal(rawMeta, &meta); err != nil {
			meta = make(map[string]any)
		}
	} else {
		meta = make(map[string]any)
	}

	if jobID != "" {
		meta["job_id"] = jobID
	}
	if correlationID != "" {
		meta["correlation_id"] = correlationID
	}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return result
	}
	top["meta"] = metaBytes

	out, err := json.Marshal(top)
	if err != nil {
		return result
	}
	return out
}
