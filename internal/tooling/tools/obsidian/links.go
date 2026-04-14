package obsidian

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	headingPattern  = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)
	wikilinkPattern = regexp.MustCompile(`!?` + `\[\[([^\]]+)\]\]`)
)

// LinkHeading captures a markdown heading and its anchor.
type LinkHeading struct {
	Level  int    `json:"level"`
	Text   string `json:"text"`
	Anchor string `json:"anchor"`
	Line   int    `json:"line"`
}

// LinkRef captures a parsed Obsidian wikilink.
type LinkRef struct {
	Raw     string `json:"raw"`
	Target  string `json:"target"`
	Alias   string `json:"alias,omitempty"`
	Subpath string `json:"subpath,omitempty"`
	IsEmbed bool   `json:"is_embed"`
	Line    int    `json:"line"`
}

// LinkParseResult is the extracted metadata for one note.
type LinkParseResult struct {
	Path     string        `json:"path"`
	Title    string        `json:"title"`
	Aliases  []string      `json:"aliases,omitempty"`
	Headings []LinkHeading `json:"headings,omitempty"`
	Outgoing []LinkRef     `json:"outgoing,omitempty"`
}

// LinkQueryOptions controls related-note lookup.
type LinkQueryOptions struct {
	Depth         int  `json:"depth"`
	IncludeDirect bool `json:"include_direct"`
	IncludeBack   bool `json:"include_backlinks"`
	IncludeAlias  bool `json:"include_aliases"`
	Limit         int  `json:"limit"`
}

// LinkQueryResult describes one related-note match.
type LinkQueryResult struct {
	Path   string   `json:"path"`
	Title  string   `json:"title"`
	Score  int      `json:"score"`
	Why    []string `json:"why"`
	Source string   `json:"source"`
}

// ParseNoteLinks extracts headings, aliases, and wikilinks from markdown content.
func ParseNoteLinks(path string, content []byte) LinkParseResult {
	result := LinkParseResult{
		Path:  filepath.Clean(path),
		Title: noteTitle(path),
	}

	aliases, body := extractAliasesAndBody(content)
	result.Aliases = aliases

	scanner := bufio.NewScanner(strings.NewReader(body))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if m := headingPattern.FindStringSubmatch(line); len(m) == 3 {
			text := strings.TrimSpace(m[2])
			result.Headings = append(result.Headings, LinkHeading{
				Level:  len(m[1]),
				Text:   text,
				Anchor: headingAnchor(text),
				Line:   lineNo,
			})
		}

		matches := wikilinkPattern.FindAllStringSubmatchIndex(line, -1)
		for _, match := range matches {
			raw := line[match[0]:match[1]]
			payload := line[match[2]:match[3]]
			ref := parseWikiLink(raw, payload, lineNo)
			result.Outgoing = append(result.Outgoing, ref)
		}
	}
	return result
}

// RelatedNotes scans a vault directory and returns notes related to the seed note by direct links, backlinks, and aliases.
func RelatedNotes(vaultRoot, notePath string, opts LinkQueryOptions) ([]LinkQueryResult, error) {
	vaultRoot = filepath.Clean(vaultRoot)
	seedAbs := notePath
	if !filepath.IsAbs(seedAbs) {
		seedAbs = filepath.Join(vaultRoot, notePath)
	}
	seedAbs = filepath.Clean(seedAbs)

	content, err := os.ReadFile(seedAbs)
	if err != nil {
		return nil, fmt.Errorf("read seed note: %w", err)
	}
	seed := ParseNoteLinks(seedAbs, content)

	if opts.Depth <= 0 {
		opts.Depth = 1
	}
	if opts.Limit <= 0 {
		opts.Limit = 25
	}
	if !opts.IncludeDirect && !opts.IncludeBack && !opts.IncludeAlias {
		opts.IncludeDirect = true
		opts.IncludeBack = true
		opts.IncludeAlias = true
	}

	notes, err := loadVaultNotes(vaultRoot)
	if err != nil {
		return nil, err
	}

	seedName := canonicalNoteName(seed.Path)
	seedAliases := make(map[string]struct{}, len(seed.Aliases)+1)
	seedAliases[seedName] = struct{}{}
	for _, alias := range seed.Aliases {
		seedAliases[strings.ToLower(strings.TrimSpace(alias))] = struct{}{}
	}

	type acc struct {
		path   string
		title  string
		score  int
		whySet map[string]struct{}
	}
	accum := make(map[string]*acc)

	add := func(path, title, why string, score int) {
		if path == seed.Path {
			return
		}
		key := filepath.Clean(path)
		item, ok := accum[key]
		if !ok {
			item = &acc{path: key, title: title, whySet: map[string]struct{}{}}
			accum[key] = item
		}
		item.score += score
		item.whySet[why] = struct{}{}
	}

	for _, note := range notes {
		for _, link := range seed.Outgoing {
			if noteMatchesLink(note, link) && opts.IncludeDirect {
				add(note.Path, note.Title, "direct_link", 3)
			}
		}

		if opts.IncludeBack {
			for _, link := range note.Outgoing {
				if _, ok := seedAliases[canonicalLinkTarget(link)]; ok {
					add(note.Path, note.Title, "backlink", 4)
				}
			}
		}

		if opts.IncludeAlias {
			for _, alias := range note.Aliases {
				if _, ok := seedAliases[strings.ToLower(strings.TrimSpace(alias))]; ok {
					add(note.Path, note.Title, "shared_alias", 2)
				}
			}
		}
	}

	results := make([]LinkQueryResult, 0, len(accum))
	for _, item := range accum {
		why := make([]string, 0, len(item.whySet))
		for reason := range item.whySet {
			why = append(why, reason)
		}
		sort.Strings(why)
		results = append(results, LinkQueryResult{
			Path:   item.path,
			Title:  item.title,
			Score:  item.score,
			Why:    why,
			Source: filepath.ToSlash(item.path),
		})
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Path < results[j].Path
	})
	if len(results) > opts.Limit {
		results = results[:opts.Limit]
	}
	return results, nil
}

func loadVaultNotes(root string) ([]LinkParseResult, error) {
	var notes []LinkParseResult
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".obsidian" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		notes = append(notes, ParseNoteLinks(path, body))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk vault notes: %w", err)
	}
	return notes, nil
}

func extractAliasesAndBody(content []byte) ([]string, string) {
	text := string(content)
	if !strings.HasPrefix(text, "---\n") {
		return nil, text
	}
	parts := strings.SplitN(text, "\n---\n", 2)
	if len(parts) != 2 {
		return nil, text
	}
	frontmatter := parts[0]
	body := parts[1]

	var aliases []string
	lines := strings.Split(frontmatter, "\n")
	inAliases := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "aliases:"):
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "aliases:"))
			if value != "" && value != "[]" {
				aliases = append(aliases, cleanAlias(value))
			}
			inAliases = value == ""
		case inAliases && strings.HasPrefix(trimmed, "-"):
			aliases = append(aliases, cleanAlias(strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))))
		case inAliases && trimmed != "":
			inAliases = false
		}
	}
	return uniqueTrimmed(aliases), body
}

func parseWikiLink(raw, payload string, lineNo int) LinkRef {
	ref := LinkRef{
		Raw:     raw,
		IsEmbed: strings.HasPrefix(raw, "!"),
		Line:    lineNo,
	}
	if parts := strings.SplitN(payload, "|", 2); len(parts) == 2 {
		ref.Target = strings.TrimSpace(parts[0])
		ref.Alias = strings.TrimSpace(parts[1])
	} else {
		ref.Target = strings.TrimSpace(payload)
	}
	if parts := strings.SplitN(ref.Target, "#", 2); len(parts) == 2 {
		ref.Target = strings.TrimSpace(parts[0])
		ref.Subpath = strings.TrimSpace(parts[1])
	}
	return ref
}

func noteMatchesLink(note LinkParseResult, link LinkRef) bool {
	target := canonicalLinkTarget(link)
	if target == "" {
		return false
	}
	if canonicalNoteName(note.Path) == target {
		return true
	}
	for _, alias := range note.Aliases {
		if strings.ToLower(strings.TrimSpace(alias)) == target {
			return true
		}
	}
	return false
}

func canonicalLinkTarget(link LinkRef) string {
	return canonicalTarget(link.Target)
}

func canonicalTarget(target string) string {
	target = strings.TrimSpace(target)
	target = strings.TrimSuffix(target, filepath.Ext(target))
	target = filepath.Base(target)
	return strings.ToLower(target)
}

func canonicalNoteName(path string) string {
	return canonicalTarget(noteTitle(path))
}

func noteTitle(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

func headingAnchor(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	text = strings.ReplaceAll(text, "_", " ")
	text = strings.Join(strings.Fields(text), "-")
	return text
}

func cleanAlias(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'[]`)
	return value
}

func uniqueTrimmed(items []string) []string {
	seen := map[string]struct{}{}
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
