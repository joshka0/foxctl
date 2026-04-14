package evidence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	refsnapshot "github.com/joshka0/foxctl/internal/intelligence/refactor/snapshot"
	refsnapshotstore "github.com/joshka0/foxctl/internal/intelligence/refactor/snapshotstore"
	"github.com/joshka0/foxctl/internal/storage/cas"
)

type ArtifactKind string

const (
	ArtifactKindSnapshot    ArtifactKind = "snapshot"
	ArtifactKindHotspotPack ArtifactKind = "hotspot_pack"
)

type ErrorKind string

const (
	ErrorKindInvalidInput    ErrorKind = "invalid_input"
	ErrorKindNotFound        ErrorKind = "not_found"
	ErrorKindInvalidArtifact ErrorKind = "invalid_artifact"
)

// LoadError reports a user-correctable refactor evidence load failure.
type LoadError struct {
	Kind    ErrorKind
	Message string
	Hint    string
}

func (e *LoadError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// HotspotPack is the persisted scout hotspot evidence artifact.
type HotspotPack struct {
	SnapshotID       string       `json:"snapshot_id"`
	SnapshotArtifact string       `json:"snapshot_artifact,omitempty"`
	IndexMode        string       `json:"index_mode"`
	Reasons          []string     `json:"reasons,omitempty"`
	Hotspots         []HotspotRow `json:"hotspots,omitempty"`
}

// HotspotRow captures one hotspot row inside a persisted scout evidence pack.
type HotspotRow struct {
	File              string   `json:"file"`
	Symbol            string   `json:"symbol"`
	RuleID            string   `json:"rule_id"`
	SeedNodeID        string   `json:"seed_node_id,omitempty"`
	SeedQuery         string   `json:"seed_query,omitempty"`
	ReverseDepCount   int      `json:"reverse_dep_count"`
	ForwardDepCount   int      `json:"forward_dep_count"`
	RecentChangeCount int      `json:"recent_change_count"`
	HotScore          float64  `json:"hot_score"`
	SymbolTouchCount  int      `json:"symbol_touch_count,omitempty"`
	SymbolHotScore    float64  `json:"symbol_hot_score,omitempty"`
	SymbolChangedLine int      `json:"symbol_changed_line_count,omitempty"`
	CochangeStrength  float64  `json:"cochange_strength,omitempty"`
	CochangeCount     int      `json:"cochange_count,omitempty"`
	CochangePaths     []string `json:"cochange_paths,omitempty"`
	SuggestedBoundary string   `json:"suggested_boundary_kind,omitempty"`
	OpportunityScore  int      `json:"opportunity_score,omitempty"`
	SuggestedReads    []string `json:"suggested_reads,omitempty"`
}

// Options controls refactor evidence loading.
type Options struct {
	Artifact   string
	SnapshotID string
}

// Result is the decoded refactor artifact payload.
type Result struct {
	Kind           ArtifactKind             `json:"kind"`
	Artifact       string                   `json:"artifact"`
	SnapshotID     string                   `json:"snapshot_id,omitempty"`
	SnapshotRecord *refsnapshotstore.Record `json:"snapshot_record,omitempty"`
	Snapshot       *refsnapshot.Payload     `json:"snapshot,omitempty"`
	HotspotPack    *HotspotPack             `json:"hotspot_pack,omitempty"`
}

// Load resolves and decodes a persisted refactor snapshot or scout evidence artifact.
func Load(ctx context.Context, storageRoot, casRoot string, opts Options) (Result, error) {
	artifact := strings.TrimSpace(opts.Artifact)
	snapshotID := strings.TrimSpace(opts.SnapshotID)
	switch {
	case artifact != "" && snapshotID != "":
		return Result{}, &LoadError{
			Kind:    ErrorKindInvalidInput,
			Message: "use either --artifact or --snapshot-id, not both",
			Hint:    "Pass the digest from `refactor scout` or `refactor snapshot`, or pass a snapshot id from `refactor snapshot`.",
		}
	case artifact == "" && snapshotID == "":
		return Result{}, &LoadError{
			Kind:    ErrorKindInvalidInput,
			Message: "either --artifact or --snapshot-id is required",
			Hint:    "Use --snapshot-id refsnap-... for a stored snapshot, or --artifact sha256:... for a snapshot/evidence artifact digest.",
		}
	}

	result := Result{}
	if snapshotID != "" {
		record, err := loadSnapshotRecord(ctx, storageRoot, snapshotID)
		if err != nil {
			return Result{}, err
		}
		artifact = record.ArtifactDigest
		result.SnapshotRecord = &record
	}
	body, err := readArtifact(ctx, casRoot, artifact)
	if err != nil {
		return Result{}, err
	}

	kind, err := detectArtifactKind(body)
	if err != nil {
		return Result{}, err
	}
	result.Kind = kind
	result.Artifact = artifact

	switch kind {
	case ArtifactKindSnapshot:
		var payload refsnapshot.Payload
		if err := json.Unmarshal(body, &payload); err != nil {
			return Result{}, fmt.Errorf("decode refactor snapshot artifact: %w", err)
		}
		result.Snapshot = &payload
		result.SnapshotID = strings.TrimSpace(payload.SnapshotID)
		if snapshotID != "" && result.SnapshotID != snapshotID {
			return Result{}, &LoadError{
				Kind:    ErrorKindInvalidArtifact,
				Message: fmt.Sprintf("snapshot artifact %q does not match snapshot id %q", artifact, snapshotID),
				Hint:    "Re-run `foxctl refactor snapshot ...` and use the returned snapshot id or artifact digest together.",
			}
		}
		if result.SnapshotRecord == nil && result.SnapshotID != "" {
			if record, err := maybeLoadSnapshotRecord(ctx, storageRoot, result.SnapshotID); err == nil && record != nil {
				result.SnapshotRecord = record
			}
		}
	case ArtifactKindHotspotPack:
		var pack HotspotPack
		if err := json.Unmarshal(body, &pack); err != nil {
			return Result{}, fmt.Errorf("decode refactor hotspot evidence artifact: %w", err)
		}
		result.HotspotPack = &pack
		result.SnapshotID = strings.TrimSpace(pack.SnapshotID)
		if result.SnapshotRecord == nil && result.SnapshotID != "" {
			if record, err := maybeLoadSnapshotRecord(ctx, storageRoot, result.SnapshotID); err == nil && record != nil {
				result.SnapshotRecord = record
			}
		}
	default:
		return Result{}, &LoadError{
			Kind:    ErrorKindInvalidArtifact,
			Message: fmt.Sprintf("artifact %q is not a supported refactor artifact", artifact),
			Hint:    "Pass a digest from `refactor snapshot` (`data.artifact`) or `refactor scout` (`data.snapshot_artifact` or `data.evidence_artifact`).",
		}
	}

	return result, nil
}

func loadSnapshotRecord(ctx context.Context, storageRoot, snapshotID string) (refsnapshotstore.Record, error) {
	store, err := refsnapshotstore.Open(ctx, storageRoot)
	if err != nil {
		return refsnapshotstore.Record{}, fmt.Errorf("open refactor snapshot store: %w", err)
	}
	defer store.Close()
	record, err := store.Get(ctx, snapshotID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return refsnapshotstore.Record{}, &LoadError{
				Kind:    ErrorKindNotFound,
				Message: fmt.Sprintf("snapshot %q not found", snapshotID),
				Hint:    "Create a snapshot first with `foxctl refactor snapshot ...`, then re-run `refactor evidence --snapshot-id <id>`.",
			}
		}
		return refsnapshotstore.Record{}, fmt.Errorf("read refactor snapshot metadata: %w", err)
	}
	return record, nil
}

func maybeLoadSnapshotRecord(ctx context.Context, storageRoot, snapshotID string) (*refsnapshotstore.Record, error) {
	record, err := loadSnapshotRecord(ctx, storageRoot, snapshotID)
	if err != nil {
		if loadErr, ok := err.(*LoadError); ok && loadErr.Kind == ErrorKindNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

func readArtifact(ctx context.Context, casRoot, artifact string) ([]byte, error) {
	store, err := cas.NewStore(strings.TrimSpace(casRoot))
	if err != nil {
		return nil, fmt.Errorf("open CAS store: %w", err)
	}
	defer store.Close()

	rc, _, err := store.Get(ctx, strings.TrimSpace(artifact))
	if err != nil {
		if errors.Is(err, cas.ErrNotFound) {
			return nil, &LoadError{
				Kind:    ErrorKindNotFound,
				Message: fmt.Sprintf("artifact %q not found", strings.TrimSpace(artifact)),
				Hint:    "Pass a digest returned by `refactor snapshot` or `refactor scout`, and ensure you are using the same ~/.foxctl CAS store.",
			}
		}
		return nil, fmt.Errorf("read CAS artifact: %w", err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("read CAS artifact body: %w", err)
	}
	return body, nil
}

func detectArtifactKind(body []byte) (ArtifactKind, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(body, &probe); err != nil {
		return "", &LoadError{
			Kind:    ErrorKindInvalidArtifact,
			Message: "artifact body is not valid JSON",
			Hint:    "Pass a digest from `refactor snapshot` or `refactor scout`, not an arbitrary CAS object.",
		}
	}
	switch {
	case hasKeys(probe, "snapshot_id", "mode", "scope", "summary", "files", "symbols"):
		return ArtifactKindSnapshot, nil
	case hasKeys(probe, "snapshot_id", "index_mode", "hotspots"):
		return ArtifactKindHotspotPack, nil
	default:
		return "", &LoadError{
			Kind:    ErrorKindInvalidArtifact,
			Message: "artifact is not a recognized refactor snapshot or hotspot evidence pack",
			Hint:    "Pass a digest from `refactor snapshot` (`data.artifact`) or `refactor scout` (`data.snapshot_artifact` or `data.evidence_artifact`).",
		}
	}
}

func hasKeys(probe map[string]json.RawMessage, keys ...string) bool {
	for _, key := range keys {
		if _, ok := probe[key]; !ok {
			return false
		}
	}
	return true
}
