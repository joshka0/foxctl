package repoindex

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	ladybugMetaFile      = "meta.json"
	ladybugGraphNodeFile = "nodes_graph.csv"
)

var ladybugNodeKinds = []NodeKind{
	NodePackage,
	NodeFile,
	NodeSymbol,
	NodeConcept,
}

var ladybugGraphNodeHeader = []string{
	"id",
	"kind",
	"repo_key",
	"repo_root_identity",
	"pkg",
	"file",
	"name",
	"signature",
	"span_start",
	"span_end",
	"exported",
	"doc",
	"summary",
	"hash",
	"updated_at",
	"meta_json",
}

var ladybugEdgeHeader = []string{
	"src_id",
	"dst_id",
	"weight",
	"source",
	"meta_json",
}

// LadybugExportArtifact is an in-memory projection artifact ready to write.
type LadybugExportArtifact struct {
	Files []LadybugExportFile
}

// LadybugExportFile is one deterministic projection file.
type LadybugExportFile struct {
	Name string
	Data []byte
}

type ladybugExportMeta struct {
	Format                 string   `json:"format"`
	Version                int      `json:"version"`
	ExportedAt             string   `json:"exported_at"`
	RepoKey                string   `json:"repo_key"`
	RepoRootIdentity       string   `json:"repo_root_identity"`
	RepoindexSchemaVersion int      `json:"repoindex_schema_version"`
	HeadSHA                string   `json:"head_sha"`
	WorktreeDirty          bool     `json:"worktree_dirty"`
	DirtyStatusHash        string   `json:"dirty_status_hash"`
	DefaultRef             string   `json:"default_ref"`
	DefaultRefSHA          string   `json:"default_ref_sha"`
	MergeBaseSHA           string   `json:"merge_base_sha"`
	CommitsAhead           int      `json:"commits_ahead"`
	CommitsBehind          int      `json:"commits_behind"`
	IndexedAt              string   `json:"indexed_at"`
	Languages              []string `json:"languages"`
}

// BuildLadybugExport builds the dependency-free typed CSV projection from a
// repoindex snapshot. The caller supplies export time so the core stays
// deterministic and testable.
func BuildLadybugExport(snapshot ExportSnapshot, exportedAt time.Time) (LadybugExportArtifact, error) {
	repoKey := strings.TrimSpace(snapshot.RepoKey)
	if repoKey == "" {
		return LadybugExportArtifact{}, fmt.Errorf("repoindex: repo key is required for ladybug export")
	}
	snapshot.RepoKey = repoKey
	if exportedAt.IsZero() {
		return LadybugExportArtifact{}, fmt.Errorf("repoindex: export time is required for ladybug export")
	}

	files := make([]LadybugExportFile, 0, 2)
	meta, err := buildLadybugMeta(snapshot, exportedAt)
	if err != nil {
		return LadybugExportArtifact{}, err
	}
	files = append(files, meta)

	nodesCSV, err := buildLadybugGraphNodeCSV(snapshot)
	if err != nil {
		return LadybugExportArtifact{}, err
	}
	files = append(files, nodesCSV)

	edgesByType := groupLadybugEdges(snapshot.Edges)
	edgeTypes := make([]EdgeType, 0, len(edgesByType))
	for edgeType := range edgesByType {
		edgeTypes = append(edgeTypes, edgeType)
	}
	sort.Slice(edgeTypes, func(i, j int) bool {
		return edgeTypes[i] < edgeTypes[j]
	})
	for _, edgeType := range edgeTypes {
		file, err := buildLadybugEdgeCSV(edgeType, edgesByType[edgeType])
		if err != nil {
			return LadybugExportArtifact{}, err
		}
		files = append(files, file)
	}

	return LadybugExportArtifact{Files: files}, nil
}

func buildLadybugMeta(snapshot ExportSnapshot, exportedAt time.Time) (LadybugExportFile, error) {
	meta := ladybugExportMeta{
		Format:                 "ladybugdb-repoindex-export",
		Version:                1,
		ExportedAt:             formatLadybugTime(exportedAt),
		RepoKey:                snapshot.RepoKey,
		RepoRootIdentity:       snapshot.RepoKey,
		RepoindexSchemaVersion: snapshot.Meta.SchemaVersion,
		HeadSHA:                snapshot.Meta.HeadSHA,
		WorktreeDirty:          snapshot.Meta.WorktreeDirty,
		DirtyStatusHash:        snapshot.Meta.DirtyStatusHash,
		DefaultRef:             snapshot.Meta.DefaultRef,
		DefaultRefSHA:          snapshot.Meta.DefaultRefSHA,
		MergeBaseSHA:           snapshot.Meta.MergeBaseSHA,
		CommitsAhead:           snapshot.Meta.CommitsAhead,
		CommitsBehind:          snapshot.Meta.CommitsBehind,
		IndexedAt:              formatLadybugTime(snapshot.Meta.IndexedAt),
		Languages:              append([]string(nil), snapshot.Meta.Languages...),
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return LadybugExportFile{}, fmt.Errorf("repoindex: build ladybug export metadata: %w", err)
	}
	data = append(data, '\n')
	return LadybugExportFile{Name: ladybugMetaFile, Data: data}, nil
}

func buildLadybugGraphNodeCSV(snapshot ExportSnapshot) (LadybugExportFile, error) {
	rows := [][]string{{
		snapshot.RepoKey,
		"repo",
		snapshot.RepoKey,
		snapshot.RepoKey,
		"",
		"",
		snapshot.RepoKey,
		"",
		"0",
		"0",
		"false",
		"",
		"",
		"",
		"",
		"",
	}}

	nodesByKind := groupLadybugNodes(snapshot.Nodes)
	for _, kind := range ladybugNodeKinds {
		nodes := nodesByKind[kind]
		sort.Slice(nodes, func(i, j int) bool {
			return compareLadybugNodes(nodes[i], nodes[j]) < 0
		})
		for _, node := range nodes {
			metaJSON, err := canonicalLadybugJSON(node.Meta, "node meta "+node.ID)
			if err != nil {
				return LadybugExportFile{}, err
			}
			rows = append(rows, []string{
				node.ID,
				string(node.Kind),
				snapshot.RepoKey,
				snapshot.RepoKey,
				node.Pkg,
				node.File,
				node.Name,
				node.Signature,
				strconv.Itoa(node.SpanStart),
				strconv.Itoa(node.SpanEnd),
				strconv.FormatBool(node.Exported),
				node.Doc,
				node.Summary,
				node.Hash,
				formatLadybugTime(node.UpdatedAt),
				metaJSON,
			})
		}
	}

	data, err := encodeLadybugCSV(ladybugGraphNodeHeader, rows)
	if err != nil {
		return LadybugExportFile{}, err
	}
	return LadybugExportFile{Name: ladybugGraphNodeFile, Data: data}, nil
}

func buildLadybugEdgeCSV(edgeType EdgeType, edges []Edge) (LadybugExportFile, error) {
	sort.Slice(edges, func(i, j int) bool {
		return compareLadybugEdges(edges[i], edges[j]) < 0
	})
	rows := make([][]string, 0, len(edges))
	for _, edge := range edges {
		metaJSON, err := canonicalLadybugJSON(edge.Meta, "edge meta "+edge.Src+" -> "+edge.Dst+" "+string(edge.Type))
		if err != nil {
			return LadybugExportFile{}, err
		}
		rows = append(rows, []string{
			edge.Src,
			edge.Dst,
			strconv.FormatFloat(edge.Weight, 'g', -1, 64),
			"repoindex",
			metaJSON,
		})
	}
	data, err := encodeLadybugCSV(ladybugEdgeHeader, rows)
	if err != nil {
		return LadybugExportFile{}, err
	}
	return LadybugExportFile{Name: ladybugEdgeFileName(edgeType), Data: data}, nil
}

func groupLadybugNodes(nodes []Node) map[NodeKind][]Node {
	grouped := make(map[NodeKind][]Node)
	for _, node := range nodes {
		grouped[node.Kind] = append(grouped[node.Kind], node)
	}
	return grouped
}

func groupLadybugEdges(edges []Edge) map[EdgeType][]Edge {
	grouped := make(map[EdgeType][]Edge)
	for _, edge := range edges {
		grouped[edge.Type] = append(grouped[edge.Type], edge)
	}
	return grouped
}

func encodeLadybugCSV(header []string, rows [][]string) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write(header); err != nil {
		return nil, fmt.Errorf("repoindex: write ladybug csv header: %w", err)
	}
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			return nil, fmt.Errorf("repoindex: write ladybug csv row: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("repoindex: write ladybug csv: %w", err)
	}
	return buf.Bytes(), nil
}

func canonicalLadybugJSON(raw json.RawMessage, label string) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", fmt.Errorf("repoindex: invalid %s JSON: %w", label, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return "", fmt.Errorf("repoindex: invalid %s JSON: multiple JSON values", label)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("repoindex: canonicalize %s JSON: %w", label, err)
	}
	return string(data), nil
}

func ladybugEdgeFileName(edgeType EdgeType) string {
	return "edges_" + strings.ToLower(string(edgeType)) + ".csv"
}

func compareLadybugNodes(a, b Node) int {
	for _, value := range []int{
		strings.Compare(a.Pkg, b.Pkg),
		strings.Compare(a.File, b.File),
		strings.Compare(a.Name, b.Name),
		strings.Compare(a.ID, b.ID),
	} {
		if value != 0 {
			return value
		}
	}
	return 0
}

func compareLadybugEdges(a, b Edge) int {
	for _, value := range []int{
		strings.Compare(a.Src, b.Src),
		strings.Compare(a.Dst, b.Dst),
		strings.Compare(string(a.Type), string(b.Type)),
	} {
		if value != 0 {
			return value
		}
	}
	if a.Weight < b.Weight {
		return -1
	}
	if a.Weight > b.Weight {
		return 1
	}
	return strings.Compare(string(a.Meta), string(b.Meta))
}

func formatLadybugTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
