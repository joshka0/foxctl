package memorycore

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type CuratorAction string

const (
	CuratorActionDemoteStale       CuratorAction = "demote_stale"
	CuratorActionArchive           CuratorAction = "archive"
	CuratorActionDeprecate         CuratorAction = "deprecate"
	CuratorActionRevalidate        CuratorAction = "revalidate"
	CuratorActionSkipPinned        CuratorAction = "skip_pinned"
	CuratorActionReviewQuarantined CuratorAction = "review_quarantined"
)

type CuratorConfig struct {
	Now                          time.Time `json:"now,omitempty"`
	StaleAfterDays               int       `json:"stale_after_days"`
	ArchiveAfterDays             int       `json:"archive_after_days"`
	RevalidateAfterDays          int       `json:"revalidate_after_days"`
	RevalidateEnvClaimsAfterDays int       `json:"revalidate_env_claims_after_days"`
	MinUsesBeforeUtilityJudgment int       `json:"min_uses_before_utility_judgment"`
	MinSuccessRateForActive      float64   `json:"min_success_rate_for_active"`
}

type CuratorSummary struct {
	TotalRecords           int            `json:"total_records"`
	ByLifecycle            map[string]int `json:"by_lifecycle,omitempty"`
	ByKind                 map[string]int `json:"by_kind,omitempty"`
	BySourceLane           map[string]int `json:"by_source_lane,omitempty"`
	ProposedDemotions      int            `json:"proposed_demotions"`
	ProposedArchives       int            `json:"proposed_archives"`
	ProposedDeprecations   int            `json:"proposed_deprecations"`
	RevalidationCandidates int            `json:"revalidation_candidates"`
	PinnedSkipped          int            `json:"pinned_skipped"`
	QuarantinedRecords     int            `json:"quarantined_records"`
	DuplicateClusters      int            `json:"duplicate_clusters"`
	OverlapClusters        int            `json:"overlap_clusters"`
	SupersessionProposals  int            `json:"supersession_proposals"`
}

type CuratorProposal struct {
	RecordID      string         `json:"record_id"`
	SourceLane    SourceLane     `json:"source_lane"`
	SourceID      string         `json:"source_id,omitempty"`
	Kind          Kind           `json:"kind"`
	Summary       string         `json:"summary,omitempty"`
	CurrentState  LifecycleState `json:"current_state"`
	ProposedState LifecycleState `json:"proposed_state,omitempty"`
	Action        CuratorAction  `json:"action"`
	Reasons       []string       `json:"reasons,omitempty"`
	Pinned        bool           `json:"pinned,omitempty"`
	UtilityScore  float64        `json:"utility_score,omitempty"`
}

type CuratorReport struct {
	ID                    string                 `json:"id"`
	Mode                  string                 `json:"mode"`
	GeneratedAt           string                 `json:"generated_at,omitempty"`
	Config                CuratorConfig          `json:"config"`
	Summary               CuratorSummary         `json:"summary"`
	Proposals             []CuratorProposal      `json:"proposals,omitempty"`
	PinnedSkipped         []CuratorProposal      `json:"pinned_skipped,omitempty"`
	Quarantined           []CuratorProposal      `json:"quarantined,omitempty"`
	ConsolidationClusters []ConsolidationCluster `json:"consolidation_clusters,omitempty"`
	SupersessionProposals []SupersessionProposal `json:"supersession_proposals,omitempty"`
}

type ConsolidationKind string

const (
	ConsolidationKindDuplicate ConsolidationKind = "duplicate"
	ConsolidationKindOverlap   ConsolidationKind = "overlap"
)

type ConsolidationCluster struct {
	ID              string            `json:"id"`
	Kind            ConsolidationKind `json:"kind"`
	RecordIDs       []string          `json:"record_ids"`
	PrimaryRecordID string            `json:"primary_record_id,omitempty"`
	ManualReview    bool              `json:"manual_review"`
	PinnedRecordIDs []string          `json:"pinned_record_ids,omitempty"`
	Signals         []string          `json:"signals,omitempty"`
	Reasons         []string          `json:"reasons,omitempty"`
}

type SupersessionProposal struct {
	RecordID      string   `json:"record_id"`
	SupersededBy  string   `json:"superseded_by"`
	ManualReview  bool     `json:"manual_review"`
	Pinned        bool     `json:"pinned,omitempty"`
	TargetMissing bool     `json:"target_missing,omitempty"`
	Reasons       []string `json:"reasons,omitempty"`
}

func DefaultCuratorConfig(now time.Time) CuratorConfig {
	return CuratorConfig{
		Now:                          now.UTC(),
		StaleAfterDays:               30,
		ArchiveAfterDays:             90,
		RevalidateAfterDays:          30,
		RevalidateEnvClaimsAfterDays: 14,
		MinUsesBeforeUtilityJudgment: 3,
		MinSuccessRateForActive:      0.50,
	}
}

func PlanCuratorReport(records []Record, cfg CuratorConfig) CuratorReport {
	cfg = normalizeCuratorConfig(cfg)
	report := CuratorReport{
		ID:     curatorReportID(cfg.Now),
		Mode:   "dry_run",
		Config: cfg,
		Summary: CuratorSummary{
			TotalRecords: len(records),
			ByLifecycle:  map[string]int{},
			ByKind:       map[string]int{},
			BySourceLane: map[string]int{},
		},
	}
	if !cfg.Now.IsZero() {
		report.GeneratedAt = cfg.Now.UTC().Format(time.RFC3339)
	}

	for _, record := range records {
		report.Summary.ByLifecycle[string(record.Lifecycle.State)]++
		report.Summary.ByKind[string(record.Kind)]++
		report.Summary.BySourceLane[string(record.SourceLane)]++
		if record.Lifecycle.State == LifecycleStateQuarantined {
			report.Summary.QuarantinedRecords++
			report.Quarantined = append(report.Quarantined, curatorProposal(record, CuratorActionReviewQuarantined, "", []string{"record is quarantined"}))
			continue
		}
		for _, proposal := range curateRecord(record, cfg) {
			if proposal.Pinned && proposal.Action != CuratorActionRevalidate {
				proposal.Action = CuratorActionSkipPinned
				proposal.ProposedState = ""
				proposal.Reasons = append(proposal.Reasons, "pinned records require manual review before mutation")
				report.Summary.PinnedSkipped++
				report.PinnedSkipped = append(report.PinnedSkipped, proposal)
				continue
			}
			switch proposal.Action {
			case CuratorActionDemoteStale:
				report.Summary.ProposedDemotions++
			case CuratorActionArchive:
				report.Summary.ProposedArchives++
			case CuratorActionDeprecate:
				report.Summary.ProposedDeprecations++
			case CuratorActionRevalidate:
				report.Summary.RevalidationCandidates++
			}
			report.Proposals = append(report.Proposals, proposal)
		}
	}
	report.ConsolidationClusters = planConsolidationClusters(records)
	report.SupersessionProposals = planSupersessionProposals(records)
	for _, cluster := range report.ConsolidationClusters {
		switch cluster.Kind {
		case ConsolidationKindDuplicate:
			report.Summary.DuplicateClusters++
		case ConsolidationKindOverlap:
			report.Summary.OverlapClusters++
		}
	}
	report.Summary.SupersessionProposals = len(report.SupersessionProposals)
	sortCuratorProposals(report.Proposals)
	sortCuratorProposals(report.PinnedSkipped)
	sortCuratorProposals(report.Quarantined)
	return report
}

func planConsolidationClusters(records []Record) []ConsolidationCluster {
	eligible := consolidationEligibleRecords(records)
	parent := map[string]string{}
	kindByPair := map[string]ConsolidationKind{}
	signalsByPair := map[string][]string{}
	for _, record := range eligible {
		parent[record.ID] = record.ID
	}
	for i := 0; i < len(eligible); i++ {
		for j := i + 1; j < len(eligible); j++ {
			left, right := eligible[i], eligible[j]
			kind, signals, ok := consolidationMatch(left, right)
			if !ok {
				continue
			}
			union(parent, left.ID, right.ID)
			key := pairKey(left.ID, right.ID)
			kindByPair[key] = kind
			signalsByPair[key] = signals
		}
	}
	members := map[string][]Record{}
	for _, record := range eligible {
		root := find(parent, record.ID)
		members[root] = append(members[root], record)
	}
	clusters := make([]ConsolidationCluster, 0)
	for _, group := range members {
		if len(group) < 2 {
			continue
		}
		sort.SliceStable(group, func(i, j int) bool { return group[i].ID < group[j].ID })
		cluster := buildConsolidationCluster(group, kindByPair, signalsByPair)
		clusters = append(clusters, cluster)
	}
	sort.SliceStable(clusters, func(i, j int) bool { return clusters[i].ID < clusters[j].ID })
	return clusters
}

func consolidationEligibleRecords(records []Record) []Record {
	out := make([]Record, 0, len(records))
	for _, record := range records {
		if strings.TrimSpace(record.ID) == "" {
			continue
		}
		switch record.Lifecycle.State {
		case LifecycleStateArchived, LifecycleStateDeprecated, LifecycleStateQuarantined:
			continue
		default:
			out = append(out, record)
		}
	}
	return out
}

func consolidationMatch(left, right Record) (ConsolidationKind, []string, bool) {
	if left.Kind != right.Kind {
		return "", nil, false
	}
	signals := []string{"same_kind"}
	if left.SourceLane == right.SourceLane {
		signals = append(signals, "same_source_lane")
	}
	if len(intersectStrings(left.Tags, right.Tags)) > 0 {
		signals = append(signals, "shared_tags")
	}
	if len(intersectStrings(recordFileRefs(left), recordFileRefs(right))) > 0 {
		signals = append(signals, "shared_file_refs")
	}
	if strings.TrimSpace(left.Lifecycle.SupersededBy) != "" && left.Lifecycle.SupersededBy == right.ID ||
		strings.TrimSpace(right.Lifecycle.SupersededBy) != "" && right.Lifecycle.SupersededBy == left.ID {
		signals = append(signals, "supersession_link")
	}
	summaryScore := summarySimilarity(left.Summary, right.Summary)
	if summaryScore >= 0.90 {
		signals = append(signals, "summary_similarity_high")
		return ConsolidationKindDuplicate, signals, true
	}
	if summaryScore >= 0.55 && len(signals) >= 3 {
		signals = append(signals, "summary_similarity_partial")
		return ConsolidationKindOverlap, signals, true
	}
	if containsString(signals, "supersession_link") {
		return ConsolidationKindOverlap, signals, true
	}
	return "", nil, false
}

func buildConsolidationCluster(records []Record, kindByPair map[string]ConsolidationKind, signalsByPair map[string][]string) ConsolidationCluster {
	ids := make([]string, 0, len(records))
	pinned := make([]string, 0)
	signals := make([]string, 0)
	kind := ConsolidationKindDuplicate
	for _, record := range records {
		ids = append(ids, record.ID)
		if record.Lifecycle.Pinned {
			pinned = append(pinned, record.ID)
		}
	}
	for i := 0; i < len(records); i++ {
		for j := i + 1; j < len(records); j++ {
			key := pairKey(records[i].ID, records[j].ID)
			if kindByPair[key] == ConsolidationKindOverlap {
				kind = ConsolidationKindOverlap
			}
			signals = append(signals, signalsByPair[key]...)
		}
	}
	signals = dedupeStrings(signals)
	sort.Strings(signals)
	sort.Strings(pinned)
	cluster := ConsolidationCluster{
		ID:              fmt.Sprintf("%s:%s", kind, strings.Join(ids, "+")),
		Kind:            kind,
		RecordIDs:       ids,
		PrimaryRecordID: choosePrimaryRecord(records),
		ManualReview:    true,
		PinnedRecordIDs: pinned,
		Signals:         signals,
		Reasons:         []string{"report-only consolidation candidate; no records are merged or patched automatically"},
	}
	if len(pinned) > 0 {
		cluster.Reasons = append(cluster.Reasons, "cluster contains pinned records that require manual review before mutation")
	}
	return cluster
}

func choosePrimaryRecord(records []Record) string {
	best := records[0]
	for _, candidate := range records[1:] {
		if candidate.Lifecycle.Pinned != best.Lifecycle.Pinned {
			if candidate.Lifecycle.Pinned {
				best = candidate
			}
			continue
		}
		if candidate.Trust.Authority != best.Trust.Authority {
			if candidate.Trust.Authority > best.Trust.Authority {
				best = candidate
			}
			continue
		}
		if candidate.Trust.Confidence != best.Trust.Confidence {
			if candidate.Trust.Confidence > best.Trust.Confidence {
				best = candidate
			}
			continue
		}
		if candidate.ID < best.ID {
			best = candidate
		}
	}
	return best.ID
}

func planSupersessionProposals(records []Record) []SupersessionProposal {
	ids := map[string]struct{}{}
	for _, record := range records {
		if strings.TrimSpace(record.ID) != "" {
			ids[record.ID] = struct{}{}
		}
	}
	out := make([]SupersessionProposal, 0)
	for _, record := range records {
		target := strings.TrimSpace(record.Lifecycle.SupersededBy)
		if target == "" || record.Lifecycle.State == LifecycleStateQuarantined {
			continue
		}
		_, targetExists := ids[target]
		proposal := SupersessionProposal{
			RecordID:      record.ID,
			SupersededBy:  target,
			ManualReview:  true,
			Pinned:        record.Lifecycle.Pinned,
			TargetMissing: !targetExists,
			Reasons:       []string{"record declares superseded_by; review before lifecycle mutation or content consolidation"},
		}
		if record.Lifecycle.Pinned {
			proposal.Reasons = append(proposal.Reasons, "pinned record requires manual review before mutation")
		}
		if !targetExists {
			proposal.Reasons = append(proposal.Reasons, "superseded_by target was not present in the report input")
		}
		out = append(out, proposal)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RecordID != out[j].RecordID {
			return out[i].RecordID < out[j].RecordID
		}
		return out[i].SupersededBy < out[j].SupersededBy
	})
	return out
}

func UtilityScore(record Record) float64 {
	uses := record.Telemetry.UseCount
	if uses <= 0 {
		return 0
	}
	successRate := float64(record.Telemetry.SuccessCount) / float64(uses)
	failureRate := float64(record.Telemetry.FailureCount) / float64(uses)
	score := 0.6*successRate + 0.2*clamp01(float64(uses)/10) - 0.4*failureRate
	return clamp01(score)
}

func curateRecord(record Record, cfg CuratorConfig) []CuratorProposal {
	var proposals []CuratorProposal
	if record.Lifecycle.State == LifecycleStateStale && ageDays(lastLifecycleTime(record), cfg.Now) >= cfg.ArchiveAfterDays {
		proposals = append(proposals, curatorProposal(record, CuratorActionArchive, LifecycleStateArchived, []string{
			fmt.Sprintf("stale for at least %d days", cfg.ArchiveAfterDays),
		}))
	}
	if shouldDemoteActiveRecord(record, cfg) {
		proposals = append(proposals, curatorProposal(record, CuratorActionDemoteStale, LifecycleStateStale, demotionReasons(record, cfg)))
	}
	if shouldRevalidateRecord(record, cfg) {
		proposals = append(proposals, curatorProposal(record, CuratorActionRevalidate, record.Lifecycle.State, revalidationReasons(record, cfg)))
	}
	return proposals
}

func shouldDemoteActiveRecord(record Record, cfg CuratorConfig) bool {
	if record.Lifecycle.State != LifecycleStateActive {
		return false
	}
	if record.Telemetry.UseCount >= cfg.MinUsesBeforeUtilityJudgment {
		successRate := float64(record.Telemetry.SuccessCount) / float64(record.Telemetry.UseCount)
		if successRate < cfg.MinSuccessRateForActive {
			return true
		}
	}
	if record.Telemetry.UseCount == 0 && ageDays(firstRecordTime(record), cfg.Now) >= cfg.StaleAfterDays {
		return true
	}
	return false
}

func shouldRevalidateRecord(record Record, cfg CuratorConfig) bool {
	if record.Lifecycle.State == LifecycleStateArchived ||
		record.Lifecycle.State == LifecycleStateDeprecated ||
		record.Lifecycle.State == LifecycleStateQuarantined {
		return false
	}
	if record.Temporal.TTLSeconds > 0 && ttlExpired(record, cfg.Now) {
		return true
	}
	lastValidated := parseRecordTime(record.Temporal.LastValidatedAt)
	if record.Temporal.RequiresValidation {
		if lastValidated.IsZero() {
			return true
		}
		return ageDays(lastValidated, cfg.Now) >= cfg.RevalidateEnvClaimsAfterDays
	}
	if !lastValidated.IsZero() && ageDays(lastValidated, cfg.Now) >= cfg.RevalidateAfterDays {
		return true
	}
	if record.Lifecycle.State == LifecycleStateCandidate && ageDays(firstRecordTime(record), cfg.Now) >= cfg.RevalidateAfterDays {
		return true
	}
	return false
}

func demotionReasons(record Record, cfg CuratorConfig) []string {
	if record.Telemetry.UseCount >= cfg.MinUsesBeforeUtilityJudgment {
		successRate := float64(record.Telemetry.SuccessCount) / float64(record.Telemetry.UseCount)
		if successRate < cfg.MinSuccessRateForActive {
			return []string{fmt.Sprintf("success rate %.2f below %.2f after %d uses", successRate, cfg.MinSuccessRateForActive, record.Telemetry.UseCount)}
		}
	}
	return []string{fmt.Sprintf("no recorded uses for at least %d days", cfg.StaleAfterDays)}
}

func revalidationReasons(record Record, cfg CuratorConfig) []string {
	if record.Temporal.TTLSeconds > 0 && ttlExpired(record, cfg.Now) {
		return []string{"record ttl has expired"}
	}
	lastValidated := parseRecordTime(record.Temporal.LastValidatedAt)
	if record.Temporal.RequiresValidation {
		if lastValidated.IsZero() {
			return []string{"record requires validation and has no last_validated_at"}
		}
		return []string{fmt.Sprintf("record requires validation and was last validated at least %d days ago", cfg.RevalidateEnvClaimsAfterDays)}
	}
	if record.Lifecycle.State == LifecycleStateCandidate {
		return []string{fmt.Sprintf("candidate older than %d days requires review", cfg.RevalidateAfterDays)}
	}
	return []string{fmt.Sprintf("last validation is at least %d days old", cfg.RevalidateAfterDays)}
}

func ttlExpired(record Record, now time.Time) bool {
	from := firstRecordTime(record)
	if from.IsZero() || now.IsZero() {
		return false
	}
	return now.Sub(from) >= time.Duration(record.Temporal.TTLSeconds)*time.Second
}

func curatorProposal(record Record, action CuratorAction, proposed LifecycleState, reasons []string) CuratorProposal {
	return CuratorProposal{
		RecordID:      record.ID,
		SourceLane:    record.SourceLane,
		SourceID:      record.SourceID,
		Kind:          record.Kind,
		Summary:       record.Summary,
		CurrentState:  record.Lifecycle.State,
		ProposedState: proposed,
		Action:        action,
		Reasons:       reasons,
		Pinned:        record.Lifecycle.Pinned,
		UtilityScore:  UtilityScore(record),
	}
}

func normalizeCuratorConfig(cfg CuratorConfig) CuratorConfig {
	if cfg.StaleAfterDays <= 0 {
		cfg.StaleAfterDays = 30
	}
	if cfg.ArchiveAfterDays <= 0 {
		cfg.ArchiveAfterDays = 90
	}
	if cfg.RevalidateAfterDays <= 0 {
		cfg.RevalidateAfterDays = 30
	}
	if cfg.RevalidateEnvClaimsAfterDays <= 0 {
		cfg.RevalidateEnvClaimsAfterDays = 14
	}
	if cfg.MinUsesBeforeUtilityJudgment <= 0 {
		cfg.MinUsesBeforeUtilityJudgment = 3
	}
	if cfg.MinSuccessRateForActive <= 0 {
		cfg.MinSuccessRateForActive = 0.50
	}
	if !cfg.Now.IsZero() {
		cfg.Now = cfg.Now.UTC()
	}
	return cfg
}

func firstRecordTime(record Record) time.Time {
	for _, raw := range []string{record.Temporal.ValidFrom, record.Temporal.ObservedAt, record.Temporal.IngestedAt} {
		if t := parseRecordTime(raw); !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

func lastLifecycleTime(record Record) time.Time {
	for _, raw := range []string{record.Temporal.LastUsedAt, record.Temporal.LastAccessedAt, record.Temporal.ValidFrom, record.Temporal.ObservedAt} {
		if t := parseRecordTime(raw); !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

func parseRecordTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

func ageDays(from, now time.Time) int {
	if from.IsZero() || now.IsZero() || now.Before(from) {
		return 0
	}
	return int(now.Sub(from).Hours() / 24)
}

func curatorReportID(now time.Time) string {
	if now.IsZero() {
		return "curator-report"
	}
	return "curator-" + now.UTC().Format("20060102T150405Z")
}

func sortCuratorProposals(proposals []CuratorProposal) {
	sort.SliceStable(proposals, func(i, j int) bool {
		if proposals[i].Action != proposals[j].Action {
			return proposals[i].Action < proposals[j].Action
		}
		if proposals[i].CurrentState != proposals[j].CurrentState {
			return proposals[i].CurrentState < proposals[j].CurrentState
		}
		return proposals[i].RecordID < proposals[j].RecordID
	})
}

func recordFileRefs(record Record) []string {
	refs := append([]string{}, record.Links.FileRefs...)
	refs = append(refs, record.Provenance.FileRefs...)
	return dedupeStrings(refs)
}

func summarySimilarity(left, right string) float64 {
	leftTerms := summaryTerms(left)
	rightTerms := summaryTerms(right)
	if len(leftTerms) == 0 || len(rightTerms) == 0 {
		return 0
	}
	intersection := 0
	for term := range leftTerms {
		if _, ok := rightTerms[term]; ok {
			intersection++
		}
	}
	union := len(leftTerms) + len(rightTerms) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func summaryTerms(summary string) map[string]struct{} {
	terms := map[string]struct{}{}
	for _, raw := range strings.FieldsFunc(strings.ToLower(summary), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if raw == "" {
			continue
		}
		terms[raw] = struct{}{}
	}
	return terms
}

func intersectStrings(left, right []string) []string {
	if len(left) == 0 || len(right) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	for _, value := range left {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	var out []string
	for _, value := range right {
		value = strings.TrimSpace(value)
		if _, ok := seen[value]; ok {
			out = append(out, value)
		}
	}
	return dedupeStrings(out)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func pairKey(left, right string) string {
	if left > right {
		left, right = right, left
	}
	return left + "\x00" + right
}

func find(parent map[string]string, id string) string {
	root := parent[id]
	for root != parent[root] {
		root = parent[root]
	}
	for id != root {
		next := parent[id]
		parent[id] = root
		id = next
	}
	return root
}

func union(parent map[string]string, left, right string) {
	leftRoot := find(parent, left)
	rightRoot := find(parent, right)
	if leftRoot == rightRoot {
		return
	}
	if leftRoot < rightRoot {
		parent[rightRoot] = leftRoot
		return
	}
	parent[leftRoot] = rightRoot
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
