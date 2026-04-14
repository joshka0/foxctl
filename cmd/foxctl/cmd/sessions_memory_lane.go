package cmd

import (
	"fmt"
	"strings"
)

type transcriptMemoryLane string

const (
	transcriptMemoryLaneDoctrine transcriptMemoryLane = "doctrine"
	transcriptMemoryLaneInsight  transcriptMemoryLane = "insight"
	transcriptMemoryLaneMixed    transcriptMemoryLane = "mixed"
)

func parseTranscriptMemoryLane(raw string) (transcriptMemoryLane, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return transcriptMemoryLaneInsight, nil
	case string(transcriptMemoryLaneDoctrine):
		return transcriptMemoryLaneDoctrine, nil
	case string(transcriptMemoryLaneInsight):
		return transcriptMemoryLaneInsight, nil
	case string(transcriptMemoryLaneMixed):
		return transcriptMemoryLaneMixed, nil
	default:
		return "", fmt.Errorf("unsupported memory lane %q", raw)
	}
}
