package obsidian

import "context"

// NoteRef identifies a note in the vault.
type NoteRef struct {
	Path  string `json:"path,omitempty"`
	Name  string `json:"name,omitempty"`
	Title string `json:"title,omitempty"`
}

// Note represents a note or a selected view of a note.
type Note struct {
	Ref         NoteRef        `json:"ref"`
	Type        string         `json:"type,omitempty"`
	Project     string         `json:"project,omitempty"`
	Status      string         `json:"status,omitempty"`
	Trust       string         `json:"trust,omitempty"`
	Frontmatter map[string]any `json:"frontmatter,omitempty"`
	Headings    []string       `json:"headings,omitempty"`
	LinksOut    []string       `json:"links_out,omitempty"`
	Content     string         `json:"content,omitempty"`
}

// SearchQuery describes a vault search request.
type SearchQuery struct {
	Query       string   `json:"query"`
	Project     string   `json:"project,omitempty"`
	Types       []string `json:"types,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Limit       int      `json:"limit,omitempty"`
	UseSemantic bool     `json:"use_semantic,omitempty"`
}

// SearchHit is a ranked note match.
type SearchHit struct {
	Ref     NoteRef `json:"ref"`
	Title   string  `json:"title,omitempty"`
	Type    string  `json:"type,omitempty"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet,omitempty"`
}

// RelatedOpts configures related-note traversal.
type RelatedOpts struct {
	Depth   int      `json:"depth,omitempty"`
	Limit   int      `json:"limit,omitempty"`
	Project string   `json:"project,omitempty"`
	Types   []string `json:"types,omitempty"`
}

// DraftNote describes a note to create in the vault.
type DraftNote struct {
	Title       string         `json:"title"`
	Type        string         `json:"type,omitempty"`
	Project     string         `json:"project,omitempty"`
	Status      string         `json:"status,omitempty"`
	Trust       string         `json:"trust,omitempty"`
	Folder      string         `json:"folder,omitempty"`
	Frontmatter map[string]any `json:"frontmatter,omitempty"`
	Body        string         `json:"body,omitempty"`
}

// AppendSectionRequest describes a bounded append under a heading.
type AppendSectionRequest struct {
	Ref             NoteRef `json:"ref"`
	Section         string  `json:"section"`
	Content         string  `json:"content"`
	CreateIfMissing bool    `json:"create_if_missing,omitempty"`
}

// KnowledgeLayer is the stable dual-plane interface the rest of foxctl should
// program against.
type KnowledgeLayer interface {
	Search(ctx context.Context, q SearchQuery) ([]SearchHit, error)
	ReadNote(ctx context.Context, ref NoteRef) (Note, error)
	Related(ctx context.Context, ref NoteRef, opts RelatedOpts) ([]NoteRef, error)
	CreateNote(ctx context.Context, note DraftNote) (NoteRef, error)
	AppendSection(ctx context.Context, req AppendSectionRequest) error
}
