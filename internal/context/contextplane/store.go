package contextplane

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/platform/timeutil"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage/obsidianindex"
	obsidiantool "github.com/joshka0/foxctl/internal/tooling/tools/obsidian"
)

// WorkspaceStore persists workspace-local ACA runtime files under .foxctl/.
type WorkspaceStore struct {
	layout Layout
}

// NewWorkspaceStore returns a file-backed control-plane store for a workspace.
func NewWorkspaceStore(workspacePath string) *WorkspaceStore {
	clean := ws.Normalize(workspacePath)
	rootDir := filepath.Join(clean, ".foxctl")
	runtimeDir := filepath.Join(rootDir, "runtime")
	queueDir := filepath.Join(runtimeDir, "queue")
	handoffsDir := filepath.Join(runtimeDir, "handoffs")
	sessionsDir := filepath.Join(runtimeDir, "sessions")
	policyDir := filepath.Join(rootDir, "policy")
	exportsDir := filepath.Join(rootDir, "exports")
	templatesDir := filepath.Join(rootDir, "templates", "obsidian-vault")

	return &WorkspaceStore{
		layout: Layout{
			WorkspacePath:          clean,
			RootDir:                rootDir,
			RuntimeDir:             runtimeDir,
			QueueDir:               queueDir,
			HandoffsDir:            handoffsDir,
			SessionsDir:            sessionsDir,
			PolicyDir:              policyDir,
			ExportsDir:             exportsDir,
			TemplatesDir:           templatesDir,
			TopOfMindPath:          filepath.Join(runtimeDir, "top_of_mind.json"),
			CurrentRunPath:         filepath.Join(runtimeDir, "current_run.json"),
			TasksQueuePath:         filepath.Join(queueDir, "tasks.ndjson"),
			BlockedQueuePath:       filepath.Join(queueDir, "blocked.ndjson"),
			ObservationsPath:       filepath.Join(runtimeDir, "observations.ndjson"),
			TensionsPath:           filepath.Join(runtimeDir, "tensions.ndjson"),
			PromotionJobsPath:      filepath.Join(runtimeDir, "promotion_jobs.ndjson"),
			MaintenanceQueuePath:   filepath.Join(runtimeDir, "maintenance_queue.ndjson"),
			EventsPath:             filepath.Join(runtimeDir, "events.ndjson"),
			RetrievalPolicyPath:    filepath.Join(policyDir, "retrieval.yaml"),
			PromotionPolicyPath:    filepath.Join(policyDir, "promotion.yaml"),
			TaskTypesPolicyPath:    filepath.Join(policyDir, "task_types.yaml"),
			OrientationExportPath:  filepath.Join(exportsDir, "latest-orientation.md"),
			ObsidianHomeIndexPath:  filepath.Join(templatesDir, "00-home", "index.md"),
			ObsidianFrontierPath:   filepath.Join(templatesDir, "00-home", "active-frontier.md"),
			ObsidianAtlasPath:      filepath.Join(templatesDir, "atlas", "projects.md"),
			ObsidianProjectMOCPath: filepath.Join(templatesDir, "notes", "moc", "project-index.md"),
			ObsidianInboxDraftsDir: filepath.Join(templatesDir, "inbox", "drafted-from-foxctl"),
		},
	}
}

// Layout returns the resolved workspace-local scaffold paths.
func (s *WorkspaceStore) Layout() Layout {
	return s.layout
}

// EnsureLayout creates the ACA workspace scaffold and seeds default policy/template files.
func (s *WorkspaceStore) EnsureLayout() (Layout, error) {
	for _, dir := range []string{
		s.layout.RootDir,
		s.layout.RuntimeDir,
		s.layout.QueueDir,
		s.layout.HandoffsDir,
		s.layout.SessionsDir,
		s.layout.PolicyDir,
		s.layout.ExportsDir,
		filepath.Dir(s.layout.ObsidianHomeIndexPath),
		filepath.Dir(s.layout.ObsidianAtlasPath),
		filepath.Dir(s.layout.ObsidianProjectMOCPath),
		s.layout.ObsidianInboxDraftsDir,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Layout{}, fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}

	if err := ensureFileWithDefault(s.layout.CurrentRunPath, "{\n  \"status\": \"idle\"\n}\n"); err != nil {
		return Layout{}, err
	}
	for _, path := range []string{
		s.layout.TasksQueuePath,
		s.layout.BlockedQueuePath,
		s.layout.ObservationsPath,
		s.layout.TensionsPath,
		s.layout.PromotionJobsPath,
		s.layout.MaintenanceQueuePath,
		s.layout.EventsPath,
	} {
		if err := ensureFileWithDefault(path, ""); err != nil {
			return Layout{}, err
		}
	}
	if err := ensureFileWithDefault(s.layout.RetrievalPolicyPath, defaultRetrievalPolicy); err != nil {
		return Layout{}, err
	}
	if err := ensureFileWithDefault(s.layout.PromotionPolicyPath, defaultPromotionPolicy); err != nil {
		return Layout{}, err
	}
	if err := ensureFileWithDefault(s.layout.TaskTypesPolicyPath, defaultTaskTypesPolicy); err != nil {
		return Layout{}, err
	}
	if err := ensureFileWithDefault(s.layout.ObsidianHomeIndexPath, defaultObsidianHomeIndex); err != nil {
		return Layout{}, err
	}
	if err := ensureFileWithDefault(s.layout.ObsidianFrontierPath, defaultObsidianFrontier); err != nil {
		return Layout{}, err
	}
	if err := ensureFileWithDefault(s.layout.ObsidianAtlasPath, defaultObsidianAtlasProjects); err != nil {
		return Layout{}, err
	}
	if err := ensureFileWithDefault(s.layout.ObsidianProjectMOCPath, defaultObsidianProjectMOC); err != nil {
		return Layout{}, err
	}

	return s.layout, nil
}

// SaveTopOfMind persists the computed bundle and writes a markdown export.
func (s *WorkspaceStore) SaveTopOfMind(top TopOfMind) (Layout, error) {
	layout, err := s.EnsureLayout()
	if err != nil {
		return Layout{}, err
	}
	if top.UpdatedAt.IsZero() {
		top.UpdatedAt = timeutil.NowUTC()
	}

	body, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return Layout{}, fmt.Errorf("marshal top_of_mind: %w", err)
	}
	body = append(body, '\n')
	if err := atomicWriteFile(layout.TopOfMindPath, body, 0o644); err != nil {
		return Layout{}, err
	}
	if err := atomicWriteFile(layout.OrientationExportPath, []byte(renderOrientationMarkdown(top)), 0o644); err != nil {
		return Layout{}, err
	}
	return layout, nil
}

// LoadTopOfMind loads the persisted orientation bundle.
func (s *WorkspaceStore) LoadTopOfMind() (TopOfMind, error) {
	body, err := os.ReadFile(s.layout.TopOfMindPath)
	if err != nil {
		return TopOfMind{}, err
	}
	var top TopOfMind
	if err := json.Unmarshal(body, &top); err != nil {
		return TopOfMind{}, fmt.Errorf("decode top_of_mind: %w", err)
	}
	return top, nil
}

// BuildReport synthesizes a current-view projection from persisted ACA state.
func (s *WorkspaceStore) BuildReport() (Report, error) {
	if _, err := s.EnsureLayout(); err != nil {
		return Report{}, err
	}
	top, err := s.LoadTopOfMind()
	if err != nil {
		return Report{}, err
	}
	handoffs, err := s.ListHandoffs(1)
	if err != nil {
		return Report{}, err
	}
	observations, err := s.ListObservations(3)
	if err != nil {
		return Report{}, err
	}
	tensions, err := s.ListTensions(10)
	if err != nil {
		return Report{}, err
	}
	openTensions := make([]Tension, 0, 3)
	for _, tension := range tensions {
		if strings.EqualFold(strings.TrimSpace(tension.Status), "closed") {
			continue
		}
		openTensions = append(openTensions, tension)
		if len(openTensions) >= 3 {
			break
		}
	}
	report := Report{
		WorkspaceID:        top.WorkspaceID,
		Objective:          top.Objective,
		Phase:              top.Phase,
		ActiveTaskIDs:      append([]string(nil), top.ActiveTaskIDs...),
		TopObservations:    observations,
		OpenTensions:       openTensions,
		GeneratedAt:        timeutil.NowUTC(),
		RecommendedActions: uniqueStrings(append(append([]string{}, top.NextActions...), top.OpenLoops...)),
	}
	if len(handoffs) > 0 {
		report.LatestHandoff = &handoffs[0]
	}
	if len(report.RecommendedActions) > 5 {
		report.RecommendedActions = report.RecommendedActions[:5]
	}
	return report, nil
}

// ListHandoffs returns the most recent handoffs first.
func (s *WorkspaceStore) ListHandoffs(limit int) ([]HandoffRecord, error) {
	if _, err := s.EnsureLayout(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.layout.HandoffsDir)
	if err != nil {
		return nil, fmt.Errorf("read handoffs dir: %w", err)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Name() > entries[j].Name()
	})
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	out := make([]HandoffRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.layout.HandoffsDir, entry.Name())
		handoff, err := s.LoadHandoff(path)
		if err != nil {
			return nil, err
		}
		out = append(out, HandoffRecord{Path: path, Handoff: handoff})
	}
	return out, nil
}

// LoadHandoff loads a handoff from an absolute path or handoff filename.
func (s *WorkspaceStore) LoadHandoff(path string) (Handoff, error) {
	if _, err := s.EnsureLayout(); err != nil {
		return Handoff{}, err
	}
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." {
		return Handoff{}, fmt.Errorf("handoff path required")
	}
	if !filepath.IsAbs(path) {
		handoffsDir := filepath.Clean(s.layout.HandoffsDir)
		if filepath.Base(path) == path {
			path = filepath.Join(handoffsDir, path)
		} else if path != handoffsDir && !strings.HasPrefix(path, handoffsDir+string(os.PathSeparator)) {
			path = filepath.Join(s.layout.WorkspacePath, path)
		}
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return Handoff{}, fmt.Errorf("read handoff %s: %w", path, err)
	}
	var handoff Handoff
	if err := json.Unmarshal(body, &handoff); err != nil {
		return Handoff{}, fmt.Errorf("decode handoff %s: %w", path, err)
	}
	return handoff, nil
}

// SaveHandoff persists a structured handoff as a timestamped JSON file.
func (s *WorkspaceStore) SaveHandoff(handoff Handoff) (string, error) {
	layout, err := s.EnsureLayout()
	if err != nil {
		return "", err
	}
	if handoff.CreatedAt.IsZero() {
		handoff.CreatedAt = timeutil.NowUTC()
	}
	handoff.EvidenceRefs = uniqueStrings(handoff.EvidenceRefs)
	handoff.FilesTouched = uniqueStrings(handoff.FilesTouched)
	handoff.Observations = uniqueStrings(handoff.Observations)
	handoff.Tensions = uniqueStrings(handoff.Tensions)
	handoff.NextActions = uniqueStrings(handoff.NextActions)
	handoff.PromotionCandidates = uniqueStrings(handoff.PromotionCandidates)

	fileName := fmt.Sprintf("%s-%s.json", safeFileSlug(handoff.TaskID, "handoff"), handoff.CreatedAt.UTC().Format("20060102T150405Z"))
	path := filepath.Join(layout.HandoffsDir, fileName)

	body, err := json.MarshalIndent(handoff, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal handoff: %w", err)
	}
	body = append(body, '\n')
	if err := atomicWriteFile(path, body, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// AppendObservation appends an observation record to observations.ndjson.
func (s *WorkspaceStore) AppendObservation(obs Observation) (string, error) {
	db, closeFn, err := s.openMutableDB(context.Background())
	if err != nil {
		return "", err
	}
	defer func() { _ = closeFn() }()
	if err := upsertObservationRow(context.Background(), db, obs); err != nil {
		return "", fmt.Errorf("upsert observation: %w", err)
	}
	return filepath.Join(s.layout.RuntimeDir, acaDBFile), nil
}

// AppendTension appends a tension record to tensions.ndjson.
func (s *WorkspaceStore) AppendTension(tension Tension) (string, error) {
	db, closeFn, err := s.openMutableDB(context.Background())
	if err != nil {
		return "", err
	}
	defer func() { _ = closeFn() }()
	if err := upsertTensionRow(context.Background(), db, tension); err != nil {
		return "", fmt.Errorf("upsert tension: %w", err)
	}
	return filepath.Join(s.layout.RuntimeDir, acaDBFile), nil
}

// ListObservations returns the most recent observations first.
func (s *WorkspaceStore) ListObservations(limit int) ([]Observation, error) {
	db, closeFn, err := s.openMutableDB(context.Background())
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()
	return listObservationRows(context.Background(), db, limit)
}

// ListTensions returns the most recent tensions first.
func (s *WorkspaceStore) ListTensions(limit int) ([]Tension, error) {
	db, closeFn, err := s.openMutableDB(context.Background())
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()
	return listTensionRows(context.Background(), db, limit)
}

// MarkPromotionJobStatusByDraft updates the tracked status for a promotion draft.
func (s *WorkspaceStore) MarkPromotionJobStatusByDraft(draftPath, status string) error {
	db, closeFn, err := s.openMutableDB(context.Background())
	if err != nil {
		return err
	}
	defer func() { _ = closeFn() }()
	if err := updatePromotionJobStatusByDraftPath(context.Background(), db, draftPath, status); err != nil {
		return fmt.Errorf("update promotion job: %w", err)
	}
	return nil
}

// DraftPromotionFromHandoff creates a markdown draft in the vault inbox and records a promotion job.
func (s *WorkspaceStore) DraftPromotionFromHandoff(path, noteType, title string) (PromotionDraftResult, error) {
	layout, err := s.EnsureLayout()
	if err != nil {
		return PromotionDraftResult{}, err
	}
	handoff, err := s.LoadHandoff(path)
	if err != nil {
		return PromotionDraftResult{}, err
	}
	if noteType = strings.TrimSpace(noteType); noteType == "" {
		noteType = "investigation"
	}
	if title = strings.TrimSpace(title); title == "" {
		title = firstNonEmpty(handoff.Summary, handoff.TaskID, "AgentCTL Promotion Draft")
	}
	draftName := fmt.Sprintf("%s-%s.md", safeFileSlug(title, "promotion-draft"), timeutil.NowUTC().Format("20060102T150405Z"))
	draftPath := filepath.Join(layout.ObsidianInboxDraftsDir, draftName)
	sourceRef := "handoff:" + filepath.Base(path)
	content := renderPromotionDraft(title, noteType, handoff, sourceRef)
	if err := atomicWriteFile(draftPath, []byte(content), 0o644); err != nil {
		return PromotionDraftResult{}, err
	}
	job := PromotionJob{
		ID:         buildRecordID("P", timeutil.NowUTC()),
		SourceRef:  sourceRef,
		SourceKind: "handoff",
		NoteType:   noteType,
		Title:      title,
		DraftPath:  draftPath,
		Status:     "drafted",
		CreatedAt:  timeutil.NowUTC(),
	}
	db, closeFn, err := s.openMutableDB(context.Background())
	if err != nil {
		return PromotionDraftResult{}, err
	}
	defer func() { _ = closeFn() }()
	if err := insertPromotionJobAndDraft(context.Background(), db, job); err != nil {
		return PromotionDraftResult{}, err
	}
	return PromotionDraftResult{
		DraftPath: draftPath,
		Job:       job,
	}, nil
}

// DraftPromotionFromObservation creates a markdown draft from a repeated observation.
func (s *WorkspaceStore) DraftPromotionFromObservation(id, noteType, title string) (PromotionDraftResult, error) {
	layout, err := s.EnsureLayout()
	if err != nil {
		return PromotionDraftResult{}, err
	}
	db, closeFn, err := s.openMutableDB(context.Background())
	if err != nil {
		return PromotionDraftResult{}, err
	}
	defer func() { _ = closeFn() }()
	selected, err := findPromotableObservationRow(context.Background(), db, id)
	if err != nil {
		return PromotionDraftResult{}, err
	}
	if selected == nil {
		return PromotionDraftResult{}, fmt.Errorf("no promotable observation found")
	}
	if selected.Count < 2 {
		return PromotionDraftResult{}, fmt.Errorf("observation %s is not repeated enough to promote", selected.ID)
	}
	if noteType = strings.TrimSpace(noteType); noteType == "" {
		noteType = "pattern"
	}
	if title = strings.TrimSpace(title); title == "" {
		title = firstNonEmpty(selected.Statement, selected.ID, "Observation Promotion Draft")
	}
	draftName := fmt.Sprintf("%s-%s.md", safeFileSlug(title, "observation-draft"), timeutil.NowUTC().Format("20060102T150405Z"))
	draftPath := filepath.Join(layout.ObsidianInboxDraftsDir, draftName)
	sourceRef := "observation:" + selected.ID
	content := renderObservationPromotionDraft(title, noteType, *selected, sourceRef)
	if err := atomicWriteFile(draftPath, []byte(content), 0o644); err != nil {
		return PromotionDraftResult{}, err
	}
	job := PromotionJob{
		ID:         buildRecordID("P", timeutil.NowUTC()),
		SourceRef:  sourceRef,
		SourceKind: "observation",
		NoteType:   noteType,
		Title:      title,
		DraftPath:  draftPath,
		Status:     "drafted",
		CreatedAt:  timeutil.NowUTC(),
	}
	if err := insertPromotionJobAndDraft(context.Background(), db, job); err != nil {
		return PromotionDraftResult{}, err
	}
	return PromotionDraftResult{DraftPath: draftPath, Job: job}, nil
}

// GenerateMaintenanceTasks derives maintenance queue items from repeated or high-impact tensions.
func (s *WorkspaceStore) GenerateMaintenanceTasks(ctx context.Context, limit int) ([]MaintenanceTask, error) {
	if _, err := s.EnsureLayout(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tensions, err := s.ListTensions(0)
	if err != nil {
		return nil, err
	}
	out := make([]MaintenanceTask, 0, len(tensions))
	now := timeutil.NowUTC()
	seq := 0
	nextStamp := func() time.Time {
		stamp := now.Add(time.Duration(seq) * time.Nanosecond)
		seq++
		return stamp
	}
	for _, tension := range tensions {
		if strings.EqualFold(strings.TrimSpace(tension.Status), "closed") {
			continue
		}
		if tension.Count < 2 && impactRank(tension.Impact) < impactRank("high") {
			continue
		}
		stamp := nextStamp()
		task := MaintenanceTask{
			ID:         buildRecordID("M", stamp),
			Title:      summarizeMaintenanceTitle(tension),
			Kind:       "maintenance",
			Priority:   maintenancePriority(tension),
			Reason:     tension.Statement,
			SourceRefs: uniqueStrings(append([]string{"tension:" + tension.ID}, tension.RelatedRefs...)),
			Status:     "open",
			CreatedAt:  stamp,
		}
		out = append(out, task)
	}
	proposals, err := s.ListMemoryProposals(ctx, 0)
	if err != nil {
		return nil, err
	}
	for _, proposal := range proposals {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		packet, ok := buildStoredPreparedProposalWorkPacket(&proposal)
		if !ok || !isLowRiskPreparedProposal(&proposal) {
			continue
		}
		stamp := nextStamp()
		task := MaintenanceTask{
			ID:       buildRecordID("M", stamp),
			Title:    summarizeProposalMaintenanceTitle(proposal, packet),
			Kind:     "proposal_merge",
			Priority: proposalMaintenancePriority(proposal),
			Reason:   proposal.Summary,
			SourceRefs: uniqueStrings([]string{
				"proposal:" + proposal.ID,
				"draft:" + packet.DraftPath,
				"target:" + packet.TargetPath,
			}),
			WorkPacket: &packet,
			Status:     "open",
			CreatedAt:  stamp,
		}
		out = append(out, task)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[j].CreatedAt.Before(out[i].CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()
	if err := replaceMaintenanceTaskRows(ctx, db, out); err != nil {
		return nil, err
	}
	return out, nil
}

// GenerateMaintenanceTasksWithHealth extends maintenance generation with vault-health findings.
func (s *WorkspaceStore) GenerateMaintenanceTasksWithHealth(ctx context.Context, limit int, health *obsidianindex.HealthReport) ([]MaintenanceTask, error) {
	tasks, err := s.GenerateMaintenanceTasks(ctx, 0)
	if err != nil {
		return nil, err
	}
	now := timeutil.NowUTC()
	seq := 0
	nextStamp := func() time.Time {
		stamp := now.Add(time.Duration(seq) * time.Nanosecond)
		seq++
		return stamp
	}
	add := func(kind, title, reason string, refs []string, priority int) {
		stamp := nextStamp()
		tasks = append(tasks, MaintenanceTask{
			ID:         buildRecordID("M", stamp),
			Title:      title,
			Kind:       kind,
			Priority:   priority,
			Reason:     reason,
			SourceRefs: uniqueStrings(refs),
			Status:     "open",
			CreatedAt:  stamp,
		})
	}
	if health != nil {
		for _, path := range health.StaleNotes {
			add("maintenance", "Review stale note: "+path, "Indexed vault health reported a stale note.", []string{"path:" + path}, 18)
		}
		for _, path := range health.OversizedMOCs {
			add("maintenance", "Trim oversized MOC: "+path, "Indexed vault health reported an oversized MOC.", []string{"path:" + path}, 22)
		}
		for _, item := range health.Unresolved {
			add("maintenance", "Resolve unresolved link: "+item, "Indexed vault health reported an unresolved link.", []string{"link:" + item}, 16)
		}
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].Priority != tasks[j].Priority {
			return tasks[i].Priority > tasks[j].Priority
		}
		return tasks[j].CreatedAt.Before(tasks[i].CreatedAt)
	})
	if limit > 0 && len(tasks) > limit {
		tasks = tasks[:limit]
	}
	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()
	if err := replaceMaintenanceTaskRows(ctx, db, tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

// ListMaintenanceTasks returns generated maintenance tasks.
func (s *WorkspaceStore) ListMaintenanceTasks(ctx context.Context, limit int) ([]MaintenanceTask, error) {
	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()
	return listMaintenanceTaskRows(ctx, db, limit)
}

// NextProposalMergeTask returns the highest-priority open proposal-merge maintenance task, if any.
func (s *WorkspaceStore) NextProposalMergeTask(ctx context.Context, limit int) (*MaintenanceTask, error) {
	if limit <= 0 {
		limit = 50
	}
	items, err := s.ListMaintenanceTasks(ctx, limit)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.Kind != "proposal_merge" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(item.Status)) {
		case "closed", "claimed":
			continue
		}
		if item.WorkPacket == nil {
			continue
		}
		copyItem := item
		return &copyItem, nil
	}
	return nil, nil
}

// RecordRetrievalCorrectionRun persists one ACA retrieval correction run summary.
func (s *WorkspaceStore) RecordRetrievalCorrectionRun(run RetrievalCorrectionRun) error {
	db, closeFn, err := s.openMutableDB(context.Background())
	if err != nil {
		return err
	}
	defer func() { _ = closeFn() }()
	if run.ID == "" {
		run.ID = buildRecordID("R", timeutil.NowUTC())
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = timeutil.NowUTC()
	}
	return insertRetrievalCorrectionRunRow(context.Background(), db, run)
}

// ListRetrievalCorrectionRuns returns persisted ACA retrieval correction runs, newest first.
func (s *WorkspaceStore) ListRetrievalCorrectionRuns(limit int) ([]RetrievalCorrectionRun, error) {
	db, closeFn, err := s.openMutableDB(context.Background())
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()
	return listRetrievalCorrectionRunRows(context.Background(), db, limit)
}

// GetRetrievalCorrectionRun returns one persisted ACA retrieval correction run by ID.
func (s *WorkspaceStore) GetRetrievalCorrectionRun(id string) (*RetrievalCorrectionRun, error) {
	db, closeFn, err := s.openMutableDB(context.Background())
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()
	return findRetrievalCorrectionRunRow(context.Background(), db, id)
}

// RecordGraphCorrectionRun persists one repoindex search or DAG correction run summary.
func (s *WorkspaceStore) RecordGraphCorrectionRun(run GraphCorrectionRun) error {
	db, closeFn, err := s.openMutableDB(context.Background())
	if err != nil {
		return err
	}
	defer func() { _ = closeFn() }()
	if run.ID == "" {
		run.ID = buildRecordID("G", timeutil.NowUTC())
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = timeutil.NowUTC()
	}
	return insertGraphCorrectionRunRow(context.Background(), db, run)
}

// ListGraphCorrectionRuns returns persisted repoindex graph correction runs, newest first.
func (s *WorkspaceStore) ListGraphCorrectionRuns(limit int) ([]GraphCorrectionRun, error) {
	db, closeFn, err := s.openMutableDB(context.Background())
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()
	return listGraphCorrectionRunRows(context.Background(), db, limit)
}

// GetGraphCorrectionRun returns one persisted repoindex graph correction run by ID.
func (s *WorkspaceStore) GetGraphCorrectionRun(id string) (*GraphCorrectionRun, error) {
	db, closeFn, err := s.openMutableDB(context.Background())
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()
	return findGraphCorrectionRunRow(context.Background(), db, id)
}

// ListPromotionJobs returns recorded promotion jobs, newest first.
func (s *WorkspaceStore) ListPromotionJobs(limit int) ([]PromotionJob, error) {
	db, closeFn, err := s.openMutableDB(context.Background())
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()
	return listPromotionJobRows(context.Background(), db, limit)
}

// ListEvidenceImportRuns returns recorded external-evidence intake runs, newest first.
func (s *WorkspaceStore) ListEvidenceImportRuns(limit int) ([]EvidenceImportRun, error) {
	db, closeFn, err := s.openMutableDB(context.Background())
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()
	return listEvidenceImportRunRows(context.Background(), db, limit)
}

// MergePromotionDraft performs an explicit reviewed merge from a local draft into a canonical vault note.
func (s *WorkspaceStore) MergePromotionDraft(ctx context.Context, vaultName, vaultPath, draftPath, targetPath, heading string) (PromotionMergeResult, error) {
	layout, err := s.EnsureLayout()
	if err != nil {
		return PromotionMergeResult{}, err
	}
	_ = layout

	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return PromotionMergeResult{}, err
	}
	defer func() { _ = closeFn() }()

	job, err := findPromotionJobRow(ctx, db, strings.TrimSpace(draftPath))
	if err != nil {
		return PromotionMergeResult{}, err
	}
	if job == nil {
		return PromotionMergeResult{}, fmt.Errorf("no drafted promotion job found")
	}
	if strings.TrimSpace(job.DraftPath) == "" {
		return PromotionMergeResult{}, fmt.Errorf("promotion job has no draft path")
	}
	draftReadPath := strings.TrimSpace(job.DraftPath)
	if !filepath.IsAbs(draftReadPath) && strings.TrimSpace(vaultPath) != "" {
		draftReadPath = filepath.Join(vaultPath, filepath.FromSlash(draftReadPath))
	}
	body, err := os.ReadFile(draftReadPath)
	if err != nil {
		return PromotionMergeResult{}, fmt.Errorf("read draft %s: %w", job.DraftPath, err)
	}

	writer := obsidiantool.NewWriter("", vaultName, obsidiantool.DefaultPolicy())
	writer.VaultPath = vaultPath
	mergeResult, err := writer.MergeReviewedDraftContent(ctx, targetPath, heading, string(body), filepath.Base(job.DraftPath))
	if err != nil {
		return PromotionMergeResult{}, err
	}
	if err := updatePromotionJobStatusByDraftPath(ctx, db, job.DraftPath, "reviewed_merged"); err != nil {
		return PromotionMergeResult{}, fmt.Errorf("update promotion job: %w", err)
	}
	job.Status = "reviewed_merged"
	return PromotionMergeResult{
		DraftPath:   job.DraftPath,
		TargetPath:  mergeResult.TargetPath,
		Heading:     mergeResult.Heading,
		MergedAs:    mergeResult.MergedAs,
		Job:         *job,
		MergedAtUTC: timeutil.NowUTC(),
	}, nil
}

func ensureFileWithDefault(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func atomicWriteFile(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	tmpPath := fmt.Sprintf("%s.tmp-%d", path, time.Now().UnixNano())
	if err := os.WriteFile(tmpPath, content, mode); err != nil {
		return fmt.Errorf("write temp %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename %s: %w", path, err)
	}
	return nil
}

func readNDJSONFile[T any](path string, limit int, sorter func([]T)) ([]T, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		return nil, nil
	}
	lines := strings.Split(text, "\n")
	items := make([]T, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var item T
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return nil, fmt.Errorf("decode ndjson record from %s: %w", path, err)
		}
		items = append(items, item)
	}
	if sorter != nil {
		sorter(items)
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func renderOrientationMarkdown(top TopOfMind) string {
	var b strings.Builder
	b.WriteString("# Latest Orientation\n\n")
	b.WriteString(fmt.Sprintf("- Workspace: `%s`\n", top.WorkspaceID))
	b.WriteString(fmt.Sprintf("- Objective: %s\n", orFallback(top.Objective, "Orient workspace and establish next action")))
	b.WriteString(fmt.Sprintf("- Phase: `%s`\n", orFallback(top.Phase, "orient")))
	b.WriteString(fmt.Sprintf("- Updated: %s\n\n", top.UpdatedAt.UTC().Format(time.RFC3339)))

	writeListSection(&b, "Active Tasks", top.ActiveTaskIDs)
	writeListSection(&b, "Hard Constraints", top.HardConstraints)
	writeListSection(&b, "Blockers", top.Blockers)
	if len(top.RecentDecisions) > 0 {
		b.WriteString("## Recent Decisions\n\n")
		for _, item := range top.RecentDecisions {
			line := item.Text
			if item.Ref != "" {
				line = fmt.Sprintf("%s (`%s`)", line, item.Ref)
			}
			b.WriteString("- " + line + "\n")
		}
		b.WriteString("\n")
	}
	writeListSection(&b, "Open Loops", top.OpenLoops)
	writeListSection(&b, "Next Actions", top.NextActions)
	writeListSection(&b, "Relevant Refs", top.RelevantRefs)
	return b.String()
}

func renderPromotionDraft(title, noteType string, handoff Handoff, sourceRef string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("title: %s\n", title))
	b.WriteString(fmt.Sprintf("type: %s\n", noteType))
	b.WriteString("status: draft\n")
	b.WriteString("trust: raw\n")
	b.WriteString("provenance_refs:\n")
	b.WriteString(fmt.Sprintf("  - %s\n", sourceRef))
	for _, ref := range handoff.EvidenceRefs {
		b.WriteString(fmt.Sprintf("  - %s\n", ref))
	}
	b.WriteString(fmt.Sprintf("updated: %s\n", timeutil.NowUTC().Format("2006-01-02")))
	b.WriteString("---\n\n")
	b.WriteString("# " + title + "\n\n")
	b.WriteString("## Summary\n\n")
	b.WriteString(handoff.Summary + "\n\n")
	writeDraftSection(&b, "Observations", handoff.Observations)
	writeDraftSection(&b, "Tensions", handoff.Tensions)
	writeDraftSection(&b, "Next Actions", handoff.NextActions)
	writeDraftSection(&b, "Files Touched", handoff.FilesTouched)
	b.WriteString("## Promotion Notes\n\n")
	b.WriteString("- Review and merge into canonical notes only after validation.\n")
	b.WriteString("- Preserve provenance when promoting durable claims.\n")
	return b.String()
}

func renderObservationPromotionDraft(title, noteType string, obs Observation, sourceRef string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("title: %s\n", title))
	b.WriteString(fmt.Sprintf("type: %s\n", noteType))
	b.WriteString("status: draft\n")
	b.WriteString("trust: reviewed\n")
	b.WriteString("provenance_refs:\n")
	b.WriteString(fmt.Sprintf("  - %s\n", sourceRef))
	for _, ref := range obs.EvidenceRefs {
		b.WriteString(fmt.Sprintf("  - %s\n", ref))
	}
	b.WriteString(fmt.Sprintf("updated: %s\n", timeutil.NowUTC().Format("2006-01-02")))
	b.WriteString("---\n\n")
	b.WriteString("# " + title + "\n\n")
	b.WriteString("## Statement\n\n")
	b.WriteString(obs.Statement + "\n\n")
	b.WriteString("## Evidence\n\n")
	b.WriteString(fmt.Sprintf("- Confidence: %.2f\n", obs.Confidence))
	b.WriteString(fmt.Sprintf("- Count: %d\n", obs.Count))
	if obs.Project != "" {
		b.WriteString(fmt.Sprintf("- Project: %s\n", obs.Project))
	}
	if obs.Area != "" {
		b.WriteString(fmt.Sprintf("- Area: %s\n", obs.Area))
	}
	b.WriteString("\n## Promotion Notes\n\n")
	b.WriteString("- Review this repeated observation before merging into canonical notes.\n")
	b.WriteString("- Preserve provenance and linked evidence when promoting it further.\n")
	return b.String()
}

func writeDraftSection(b *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	b.WriteString("## " + title + "\n\n")
	for _, item := range items {
		b.WriteString("- " + item + "\n")
	}
	b.WriteString("\n")
}

func writeListSection(b *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	b.WriteString("## " + title + "\n\n")
	for _, item := range items {
		b.WriteString("- " + item + "\n")
	}
	b.WriteString("\n")
}

func orFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func uniqueStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func observationKey(obs Observation) string {
	return strings.ToLower(strings.TrimSpace(obs.Statement)) + "|" +
		strings.ToLower(strings.TrimSpace(obs.Project)) + "|" +
		strings.ToLower(strings.TrimSpace(obs.Area))
}

func tensionKey(t Tension) string {
	return strings.ToLower(strings.TrimSpace(t.Kind)) + "|" +
		strings.ToLower(strings.TrimSpace(t.Statement)) + "|" +
		strings.ToLower(strings.TrimSpace(t.Status))
}

func impactRank(impact string) int {
	switch strings.ToLower(strings.TrimSpace(impact)) {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func maintenancePriority(t Tension) int {
	return (impactRank(t.Impact) * 10) + t.Count
}

func proposalMaintenancePriority(proposal MemoryProposal) int {
	base := 18
	switch strings.ToLower(strings.TrimSpace(proposal.BlastRadius)) {
	case "low", "":
		base = 22
	case "medium":
		base = 18
	}
	return base + proposal.Count
}

func summarizeMaintenanceTitle(t Tension) string {
	statement := strings.TrimSpace(t.Statement)
	if statement == "" {
		return "Resolve ACA maintenance tension"
	}
	if len(statement) > 72 {
		statement = statement[:69] + "..."
	}
	return "Resolve tension: " + statement
}

func summarizeProposalMaintenanceTitle(proposal MemoryProposal, packet ProposalWorkPacket) string {
	target := strings.TrimSpace(packet.TargetPath)
	if target == "" {
		target = strings.TrimSpace(proposal.Summary)
	}
	if len(target) > 72 {
		target = target[:69] + "..."
	}
	return "Merge prepared proposal: " + target
}

func safeFileSlug(value, fallback string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return fallback
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return fallback
	}
	return out
}

func buildRecordID(prefix string, now time.Time) string {
	return fmt.Sprintf("%s-%s", prefix, now.UTC().Format("20060102T150405.000000000"))
}

const defaultRetrievalPolicy = `retrieval_order:
  - top_of_mind
  - active_task_packet
  - latest_handoff
  - procedural_memory
  - vault_retrieval
  - external_sources

budgets:
  top_of_mind_tokens: 1000
  task_packet_tokens: 500
  handoff_tokens: 400
  evidence_snippets_min: 3
  evidence_snippets_max: 7

ranking_weights:
  project_match: 4
  trust_level: 3
  note_type_weight: 3
  link_proximity: 2
  lexical_match: 2
  co_change: 3
  semantic_match: 2
  recency: 1
  reuse_frequency: 1

aca:
  package_note_fallback: false
  co_change_prior: false
  co_change_commit_limit: 40
  co_change_max_files_per_commit: 20
  co_change_half_life_days: 90
  continuity_bundles: true
`

const defaultPromotionPolicy = `trust_levels:
  - raw
  - reviewed
  - canonical

allowed_write_paths:
  - runtime_state
  - session_logs
  - inbox_drafts
  - exported_ops_snapshots

append_only_sections:
  - index_notes
  - review_sections
  - recent_findings

protected_sections:
  - adr_conclusions
  - methodology_rules
  - reviewed_incident_outcomes
`

const defaultTaskTypesPolicy = `task_types:
  - design
  - implement
  - verify
  - curate
  - promote
  - maintenance

maintenance_triggers:
  repeated_tension: true
  stale_note_review: true
  broken_link_review: true
  contradiction_review: true
`

const defaultObsidianHomeIndex = `---
title: AgentCTL Knowledge Home
type: map
status: draft
trust: reviewed
---

# AgentCTL Knowledge Home

- [[active-frontier]]
- [[projects]]
- [[project-index]]

Use this vault as the durable knowledge plane. Runtime truth stays in workspace-local .foxctl/runtime/.
`

const defaultObsidianFrontier = `---
title: Active Frontier
type: map
status: draft
trust: reviewed
---

# Active Frontier

This note is a durable mirror of the current frontier, not the authoritative runtime source.

Recommended sections:
- Current objective
- Open loops
- Canonical refs
- Promotion candidates
`

const defaultObsidianAtlasProjects = `---
title: Projects Atlas
type: map
status: draft
trust: reviewed
---

# Projects Atlas

- [[project-index]]
- [[active-frontier]]

Group project maps here and link canonical ADR, pattern, and incident indexes.
`

const defaultObsidianProjectMOC = `---
title: Project Index
type: map
project: foxctl
status: draft
trust: reviewed
---

# Project Index

## Canonical
- ADRs
- Patterns
- Incidents

## Operational Inputs
- Latest exported sessions
- Promotion inbox

## Repo Links
- Paths
- Symbols
- Investigations
`
