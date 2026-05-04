package contextengine

import (
	"context"
	"fmt"
	"strings"
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
	internalMissingEvidence := []string{}

	if err := bundle.Validate(); err != nil {
		cert := ContextCertificate{
			ID:                 opts.IDGen(),
			WorkspaceID:        bundle.WorkspaceID,
			BundleID:           bundle.ID,
			Status:             ContextCertificateStatusFailed,
			Checks:             []ContextCheck{{Name: "bundle_validate", Status: "fail", Message: err.Error()}},
			RequiredEvidenceOK: false,
			InternalEvidenceOK: false,
			AnswerContextOK:    false,
			IssuedAt:           opts.Clock(),
			ExpiresAt:          opts.ExpiresAt,
		}
		cert.Trust = &ContextTrustReport{
			Status:             "blocked",
			InternalEvidenceOK: false,
			RequiredEvidenceOK: false,
			AnswerContextOK:    false,
			FreshnessScore:     1,
			Gates: []ContextTrustGate{{
				Name:    "internal_evidence",
				Status:  "fail",
				Score:   0,
				Message: err.Error(),
			}},
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
				internalMissingEvidence = append(internalMissingEvidence, evidenceID)
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
		internalMissingEvidence = append(internalMissingEvidence, sourceDiag.BadEvidenceIDs...)
		for _, fact := range bundle.Facts {
			for _, evidenceID := range fact.EvidenceIDs {
				if _, ok := sourceDiag.BadEvidenceSet[evidenceID]; ok {
					unsupportedFacts = append(unsupportedFacts, fact.ID)
					break
				}
			}
		}
	}

	coverageMissing := coverageReportMissing(bundle.CoverageReport)
	for _, requirementID := range coverageMissing {
		missingEvidence = append(missingEvidence, "coverage:"+requirementID)
	}

	internalEvidenceOK := len(bundle.Facts) > 0 && len(unsupportedFacts) == 0 && len(internalMissingEvidence) == 0
	requiredEvidenceOK := internalEvidenceOK && len(coverageMissing) == 0
	trust := buildContextTrustReport(bundle, internalEvidenceOK, requiredEvidenceOK, uniqueStrings(staleEvidenceIDs), uniqueStrings(conflictIDs), coverageMissing)
	answerContextOK := trust.AnswerContextOK
	status := ContextCertificateStatusCertified
	if !internalEvidenceOK || len(coverageMissing) > 0 {
		status = ContextCertificateStatusFailed
	} else if !answerContextOK {
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
	if len(coverageMissing) > 0 {
		checks = append(checks, ContextCheck{Name: "coverage_requirements", Status: "fail", Message: fmt.Sprintf("%d required coverage slots missing", len(coverageMissing))})
	} else if bundle.CoverageReport != nil && len(bundle.CoverageReport.Requirements) > 0 {
		checks = append(checks, ContextCheck{Name: "coverage_requirements", Status: "pass"})
	}
	checks = append(checks, trustChecks(trust)...)

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
		InternalEvidenceOK: internalEvidenceOK,
		AnswerContextOK:    answerContextOK,
		Trust:              trust,
		IssuedAt:           opts.Clock(),
		ExpiresAt:          opts.ExpiresAt,
	}
	if err := cert.Validate(); err != nil {
		return ContextCertificate{}, err
	}
	return cert, nil
}

func coverageReportMissing(report *CoverageReport) []string {
	if report == nil || len(report.Missing) == 0 {
		return nil
	}
	out := make([]string, 0, len(report.Missing))
	for _, id := range report.Missing {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return uniqueStrings(out)
}

const graphConfidenceRecommendedThreshold = 0.70

func buildContextTrustReport(bundle ContextBundle, internalEvidenceOK, requiredEvidenceOK bool, staleEvidenceIDs, conflictIDs, coverageMissing []string) *ContextTrustReport {
	freshnessScore := freshnessTrustScore(len(bundle.Evidence), len(staleEvidenceIDs), len(conflictIDs))
	coverageScore, coverageAvailable := coverageTrustScore(bundle.CoverageReport)
	graphConfidence, graphConfidenceAvailable := bundleGraphConfidence(bundle)
	graphRecommended := false
	gates := []ContextTrustGate{{
		Name:   "internal_evidence",
		Status: passFailStatus(internalEvidenceOK),
		Score:  boolScore(internalEvidenceOK),
	}}
	if !internalEvidenceOK {
		gates[0].Message = "bundle evidence failed internal validation"
	}
	if coverageAvailable {
		gate := ContextTrustGate{
			Name:   "coverage",
			Status: passFailStatus(len(coverageMissing) == 0),
			Score:  coverageScore,
		}
		if len(coverageMissing) > 0 {
			gate.Message = fmt.Sprintf("%d required coverage slots missing", len(coverageMissing))
			gate.Missing = append([]string(nil), coverageMissing...)
		}
		gates = append(gates, gate)
	}
	freshnessGate := ContextTrustGate{
		Name:   "freshness",
		Status: "pass",
		Score:  freshnessScore,
	}
	if freshnessScore < 1 {
		freshnessGate.Status = "warn"
		freshnessGate.Message = fmt.Sprintf("%d stale evidence ids and %d conflicts", len(staleEvidenceIDs), len(conflictIDs))
		freshnessGate.Missing = append([]string(nil), staleEvidenceIDs...)
	}
	gates = append(gates, freshnessGate)

	if graphSensitiveTaskType(bundle.MetadataString("task_type")) {
		graphGate := ContextTrustGate{
			Name: "graph_confidence",
		}
		switch {
		case !graphConfidenceAvailable:
			graphRecommended = true
			graphGate.Status = "warn"
			graphGate.Score = 0
			graphGate.Message = "graph-sensitive task has no graph confidence metadata"
			graphGate.Missing = []string{"graph_confidence"}
		case graphConfidence < graphConfidenceRecommendedThreshold:
			graphRecommended = true
			graphGate.Status = "warn"
			graphGate.Score = graphConfidence
			graphGate.Message = fmt.Sprintf("graph confidence %.2f is below %.2f", graphConfidence, graphConfidenceRecommendedThreshold)
		default:
			graphGate.Status = "pass"
			graphGate.Score = graphConfidence
		}
		gates = append(gates, graphGate)
	}

	answerContextOK := requiredEvidenceOK && freshnessScore == 1 && len(bundle.Missing) == 0 && !graphRecommended
	status := "trusted"
	switch {
	case !internalEvidenceOK || len(coverageMissing) > 0:
		status = "blocked"
	case !answerContextOK:
		status = "partial"
	}
	report := &ContextTrustReport{
		Status:             status,
		InternalEvidenceOK: internalEvidenceOK,
		RequiredEvidenceOK: requiredEvidenceOK,
		AnswerContextOK:    answerContextOK,
		GraphRecommended:   graphRecommended,
		FreshnessScore:     freshnessScore,
		Gates:              gates,
	}
	if coverageAvailable {
		report.CoverageScore = coverageScore
	}
	if graphConfidenceAvailable {
		report.GraphConfidence = graphConfidence
	}
	return report
}

func trustChecks(report *ContextTrustReport) []ContextCheck {
	if report == nil {
		return nil
	}
	checks := make([]ContextCheck, 0, len(report.Gates)+1)
	for _, gate := range report.Gates {
		status := gate.Status
		if status == "" {
			status = "pass"
		}
		checks = append(checks, ContextCheck{Name: gate.Name, Status: status, Message: gate.Message})
	}
	checks = append(checks, ContextCheck{Name: "answer_context", Status: answerContextCheckStatus(report), Message: answerContextTrustMessage(report)})
	return checks
}

func answerContextCheckStatus(report *ContextTrustReport) string {
	if report == nil || report.AnswerContextOK {
		return "pass"
	}
	if report.Status == "blocked" {
		return "fail"
	}
	return "warn"
}

func answerContextTrustMessage(report *ContextTrustReport) string {
	if report == nil || report.AnswerContextOK {
		return ""
	}
	if report.GraphRecommended {
		return "answer context is partial; graph expansion recommended"
	}
	return "answer context is partial"
}

func graphSensitiveTaskType(taskType string) bool {
	switch strings.TrimSpace(strings.ToLower(taskType)) {
	case "subsystem_map", "integration_surface", "execution_trace", "change_impact":
		return true
	default:
		return false
	}
}

func coverageTrustScore(report *CoverageReport) (float64, bool) {
	if report == nil || len(report.Requirements) == 0 {
		return 0, false
	}
	required := 0
	missing := map[string]struct{}{}
	for _, id := range report.Missing {
		if id = strings.TrimSpace(id); id != "" {
			missing[id] = struct{}{}
		}
	}
	for _, req := range report.Requirements {
		if req.Required {
			required++
		}
	}
	if required == 0 {
		return 1, true
	}
	covered := required - len(missing)
	if covered < 0 {
		covered = 0
	}
	return float64(covered) / float64(required), true
}

func freshnessTrustScore(totalEvidence, staleCount, conflictCount int) float64 {
	if totalEvidence <= 0 {
		return 0
	}
	penalty := staleCount + conflictCount
	if penalty <= 0 {
		return 1
	}
	if penalty >= totalEvidence {
		return 0
	}
	return float64(totalEvidence-penalty) / float64(totalEvidence)
}

func bundleGraphConfidence(bundle ContextBundle) (float64, bool) {
	for _, key := range []string{"graph_confidence", "context_graph_confidence", "repo_graph_confidence"} {
		if value, ok := metadataFloat(bundle.Metadata, key); ok {
			return value, true
		}
	}
	for _, key := range []string{"graph_confidence", "context_graph_confidence", "graph_report"} {
		if value, ok := nestedGraphConfidence(bundle.Metadata[key]); ok {
			return value, true
		}
	}
	for _, node := range bundle.Evidence {
		for _, key := range []string{"graph_confidence", "context_graph_confidence", "repo_graph_confidence"} {
			if value, ok := metadataFloat(node.Metadata, key); ok {
				return value, true
			}
		}
	}
	return 0, false
}

func nestedGraphConfidence(value any) (float64, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range []string{"overall", "graph_coverage", "confidence"} {
			if value, ok := metadataFloatValue(typed[key]); ok {
				return value, true
			}
		}
		if nested, ok := typed["confidence"]; ok {
			return nestedGraphConfidence(nested)
		}
	case ContextGraphReport:
		return typed.Confidence.Overall, typed.Confidence.Overall > 0
	case *ContextGraphReport:
		if typed != nil {
			return typed.Confidence.Overall, typed.Confidence.Overall > 0
		}
	case ContextGraphConfidence:
		return typed.Overall, typed.Overall > 0
	case *ContextGraphConfidence:
		if typed != nil {
			return typed.Overall, typed.Overall > 0
		}
	}
	return metadataFloatValue(value)
}

func passFailStatus(ok bool) string {
	if ok {
		return "pass"
	}
	return "fail"
}

func boolScore(ok bool) float64 {
	if ok {
		return 1
	}
	return 0
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
		case EvidenceNodeTypeRetrieval:
			switch node.Ref.Type {
			case RefTypeNote, RefTypeSession, RefTypeEvent, RefTypeTask, RefTypePath, RefTypeSymbol, RefTypeMemoryClaim, RefTypeArtifact, RefTypeTrajectory, RefTypeRun, RefTypeToolCall:
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
