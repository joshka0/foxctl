package contextengine

import (
	"context"
	"fmt"
	"time"
)

// CertificationOptions configures runtime context bundle certification.
type CertificationOptions struct {
	IDGen     IDGen
	Clock     ClockFunc
	ExpiresAt time.Time
}

func (o CertificationOptions) defaults() CertificationOptions {
	if o.IDGen == nil {
		o.IDGen = defaultContextIDGen("cert")
	}
	if o.Clock == nil {
		o.Clock = func() time.Time { return time.Now().UTC() }
	}
	return o
}

// CertifyContextBundle validates bundle invariants and staleness markers.
func CertifyContextBundle(bundle ContextBundle, markers []StalenessMarker, opts CertificationOptions) (ContextCertificate, error) {
	opts = opts.defaults()

	checks := []ContextCheck{}
	unsupportedFacts := []string{}
	staleEvidenceIDs := []string{}
	conflictIDs := []string{}
	missingEvidence := []string{}

	if err := bundle.Validate(); err != nil {
		cert := ContextCertificate{
			ID:                 opts.IDGen(),
			WorkspaceID:        bundle.WorkspaceID,
			BundleID:           bundle.ID,
			Status:             ContextCertificateStatusFailed,
			Checks:             []ContextCheck{{Name: "bundle_validate", Status: "fail", Message: err.Error()}},
			RequiredEvidenceOK: false,
			IssuedAt:           opts.Clock(),
			ExpiresAt:          opts.ExpiresAt,
		}
		return cert, nil
	}
	checks = append(checks, ContextCheck{Name: "bundle_validate", Status: "pass"})

	evidenceByID := make(map[string]EvidenceNode, len(bundle.Evidence))
	for _, node := range bundle.Evidence {
		evidenceByID[node.ID] = node
	}

	markersByRef := make(map[string]StalenessMarker, len(markers))
	for _, marker := range markers {
		if marker.WorkspaceID != "" && marker.WorkspaceID != bundle.WorkspaceID {
			continue
		}
		markersByRef[evidenceRefKey(marker.TargetRef)] = marker
	}

	for _, fact := range bundle.Facts {
		if fact.Status == ContextFactStatusUnsupported {
			unsupportedFacts = append(unsupportedFacts, fact.ID)
		}
		if fact.Status == ContextFactStatusCandidate || fact.Status == ContextFactStatusStale || fact.Status == ContextFactStatusContradicted {
			staleEvidenceIDs = append(staleEvidenceIDs, fact.EvidenceIDs...)
		}
		for _, evidenceID := range fact.EvidenceIDs {
			node, ok := evidenceByID[evidenceID]
			if !ok {
				unsupportedFacts = append(unsupportedFacts, fact.ID)
				missingEvidence = append(missingEvidence, evidenceID)
				continue
			}
			marker, ok := markersByRef[evidenceRefKey(node.Ref)]
			if !ok {
				continue
			}
			switch marker.Status {
			case StalenessStatusContradicted:
				staleEvidenceIDs = append(staleEvidenceIDs, evidenceID)
				conflictIDs = append(conflictIDs, marker.ID)
			case StalenessStatusStale, StalenessStatusSuperseded:
				staleEvidenceIDs = append(staleEvidenceIDs, evidenceID)
			case StalenessStatusDirty, StalenessStatusNeedsRevalidation, StalenessStatusUnknown:
				staleEvidenceIDs = append(staleEvidenceIDs, evidenceID)
			}
		}
	}

	sourceDiag := certifyEvidenceSourceContracts(bundle)
	checks = append(checks, sourceDiag.Checks...)
	staleEvidenceIDs = append(staleEvidenceIDs, sourceDiag.StaleEvidenceIDs...)
	if len(sourceDiag.UnloadableRefs) > 0 {
		missingEvidence = append(missingEvidence, sourceDiag.BadEvidenceIDs...)
		for _, fact := range bundle.Facts {
			for _, evidenceID := range fact.EvidenceIDs {
				if _, ok := sourceDiag.BadEvidenceSet[evidenceID]; ok {
					unsupportedFacts = append(unsupportedFacts, fact.ID)
					break
				}
			}
		}
	}

	requiredEvidenceOK := len(bundle.Facts) > 0 && len(unsupportedFacts) == 0 && len(missingEvidence) == 0
	status := ContextCertificateStatusCertified
	if !requiredEvidenceOK {
		status = ContextCertificateStatusFailed
	} else if len(staleEvidenceIDs) > 0 || len(conflictIDs) > 0 || len(bundle.Missing) > 0 {
		status = ContextCertificateStatusPartial
	}

	if len(bundle.Facts) == 0 {
		checks = append(checks, ContextCheck{Name: "fact_evidence", Status: "fail", Message: "bundle has no facts"})
	} else if len(unsupportedFacts) > 0 || len(missingEvidence) > 0 {
		checks = append(checks, ContextCheck{Name: "fact_evidence", Status: "fail", Message: "one or more facts lack valid evidence"})
	} else {
		checks = append(checks, ContextCheck{Name: "fact_evidence", Status: "pass"})
	}
	if len(staleEvidenceIDs) > 0 {
		checks = append(checks, ContextCheck{Name: "staleness", Status: "warn", Message: fmt.Sprintf("%d stale evidence ids", len(staleEvidenceIDs))})
	} else {
		checks = append(checks, ContextCheck{Name: "staleness", Status: "pass"})
	}

	cert := ContextCertificate{
		ID:                 opts.IDGen(),
		WorkspaceID:        bundle.WorkspaceID,
		BundleID:           bundle.ID,
		Status:             status,
		Checks:             checks,
		UnsupportedFacts:   uniqueStrings(unsupportedFacts),
		StaleEvidenceIDs:   uniqueStrings(staleEvidenceIDs),
		UnloadableRefs:     uniqueEvidenceRefs(sourceDiag.UnloadableRefs),
		ConflictIDs:        uniqueStrings(conflictIDs),
		MissingEvidence:    uniqueStrings(missingEvidence),
		RequiredEvidenceOK: requiredEvidenceOK,
		IssuedAt:           opts.Clock(),
		ExpiresAt:          opts.ExpiresAt,
	}
	if err := cert.Validate(); err != nil {
		return ContextCertificate{}, err
	}
	return cert, nil
}

// StalenessLookupFunc returns staleness markers for refs relevant to a bundle.
type StalenessLookupFunc func(ctx context.Context, workspaceID string, refs []EvidenceRef) ([]StalenessMarker, error)

type sourceContractDiagnostics struct {
	Checks           []ContextCheck
	UnloadableRefs   []EvidenceRef
	BadEvidenceIDs   []string
	BadEvidenceSet   map[string]struct{}
	StaleEvidenceIDs []string
}

func certifyEvidenceSourceContracts(bundle ContextBundle) sourceContractDiagnostics {
	diag := sourceContractDiagnostics{BadEvidenceSet: map[string]struct{}{}}
	total := 0
	bad := 0
	warn := 0
	stale := 0
	markBad := func(node EvidenceNode) {
		bad++
		diag.UnloadableRefs = append(diag.UnloadableRefs, node.Ref)
		diag.BadEvidenceIDs = append(diag.BadEvidenceIDs, node.ID)
		diag.BadEvidenceSet[node.ID] = struct{}{}
	}
	markStale := func(node EvidenceNode) {
		stale++
		diag.StaleEvidenceIDs = append(diag.StaleEvidenceIDs, node.ID)
	}
	for _, node := range bundle.Evidence {
		total++
		switch node.NodeType {
		case EvidenceNodeTypeCode:
			if node.Ref.Type != RefTypePath && node.Ref.Type != RefTypeSymbol {
				markBad(node)
			}
			if node.Ref.Type == RefTypeSymbol && stringMetadata(node.Metadata, "path") == "" {
				warn++
			}
		case EvidenceNodeTypeMemory:
			if node.Ref.Type != RefTypeMemoryClaim {
				markBad(node)
			}
			if statusText := stringMetadata(node.Metadata, "status"); statusText != "" {
				status := ClaimStatus(statusText)
				if !status.IsValid() {
					markBad(node)
				}
				switch status {
				case ClaimStatusStale, ClaimStatusSuperseded, ClaimStatusRejected, ClaimStatusNeedsRevalidation:
					markStale(node)
				}
			}
		case EvidenceNodeTypeTask:
			if node.Ref.Type != RefTypeTask && node.Ref.Type != RefTypePath && node.Ref.Type != RefTypeSymbol {
				markBad(node)
			}
		case EvidenceNodeTypeContext:
			switch node.Ref.Type {
			case RefTypeNote, RefTypeSession, RefTypeEvent, RefTypeTask, RefTypePath, RefTypeMemoryClaim:
			default:
				markBad(node)
			}
		case EvidenceNodeTypeTrajectory:
			switch node.Ref.Type {
			case RefTypeTrajectory, RefTypeRun, RefTypeEvent, RefTypeToolCall, RefTypeArtifact:
			default:
				markBad(node)
			}
		}
		if node.Statement == "" && node.Ref.Excerpt == "" {
			warn++
		}
	}
	switch {
	case bad > 0:
		diag.Checks = append(diag.Checks, ContextCheck{Name: "source_contract", Status: "fail", Message: fmt.Sprintf("%d evidence refs violate source contracts", bad)})
	case stale > 0 || warn > 0:
		diag.Checks = append(diag.Checks, ContextCheck{Name: "source_contract", Status: "warn", Message: fmt.Sprintf("%d stale and %d weak evidence nodes across %d checked", stale, warn, total)})
	default:
		diag.Checks = append(diag.Checks, ContextCheck{Name: "source_contract", Status: "pass"})
	}
	return diag
}

func stringMetadata(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, _ := metadata[key].(string)
	return value
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, value := range in {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func uniqueEvidenceRefs(in []EvidenceRef) []EvidenceRef {
	seen := make(map[string]struct{}, len(in))
	out := make([]EvidenceRef, 0, len(in))
	for _, ref := range in {
		key := evidenceRefKey(ref)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}
