// Package builtin provides embedded knowledge assets that ship with agentctl.
// This includes Factory AI droids and orchestrator documentation.
package builtin

import (
	"bufio"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/jkatigb/agentctl/internal/storage/knowledge"
)

//go:embed data/droids/*.md
var factoryDroids embed.FS

// BuiltinAsset represents a single embedded knowledge asset.
type BuiltinAsset struct {
	// Name is the knowledge item name (e.g., "factory/droid/orchestrator").
	Name string
	// Kind is the knowledge item kind.
	Kind knowledge.ItemKind
	// Description is extracted from the asset frontmatter.
	Description string
	// SourcePath is the synthetic source path (e.g., "builtin://factory/droids/orchestrator.md").
	SourcePath string
	// Body is the full content of the asset.
	Body string
	// Keywords are extracted from the description for trigger matching.
	Keywords []string
}

// ListFactoryDroids returns all embedded Factory droid assets.
func ListFactoryDroids() ([]BuiltinAsset, error) {
	var assets []BuiltinAsset

	err := fs.WalkDir(factoryDroids, "data/droids", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}

		content, err := factoryDroids.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read embedded file %s: %w", path, err)
		}

		asset, err := parseDroidAsset(path, string(content))
		if err != nil {
			return fmt.Errorf("parse droid %s: %w", path, err)
		}

		assets = append(assets, asset)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return assets, nil
}

// parseDroidAsset parses a droid markdown file and extracts metadata.
func parseDroidAsset(path, content string) (BuiltinAsset, error) {
	// Extract filename without extension for slug
	filename := strings.TrimPrefix(path, "data/droids/")
	slug := strings.TrimSuffix(filename, ".md")

	// Parse YAML frontmatter
	name, description := parseYAMLFrontmatter(content)
	if name == "" {
		name = slug
	}

	// Generate keywords from description
	keywords := extractKeywords(description)

	return BuiltinAsset{
		Name:        fmt.Sprintf("factory/droid/%s", slug),
		Kind:        knowledge.KindAgent,
		Description: description,
		SourcePath:  fmt.Sprintf("builtin://factory/droids/%s.md", slug),
		Body:        content,
		Keywords:    keywords,
	}, nil
}

// parseYAMLFrontmatter extracts name and description from YAML frontmatter.
func parseYAMLFrontmatter(content string) (name, description string) {
	lines := strings.Split(content, "\n")
	inFrontmatter := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			if inFrontmatter {
				break // End of frontmatter
			}
			inFrontmatter = true
			continue
		}
		if !inFrontmatter {
			continue
		}

		if strings.HasPrefix(trimmed, "name:") {
			name = strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
		} else if strings.HasPrefix(trimmed, "description:") {
			description = strings.TrimSpace(strings.TrimPrefix(trimmed, "description:"))
		}
	}

	return name, description
}

// extractKeywords extracts meaningful keywords from a description.
func extractKeywords(description string) []string {
	// Simple keyword extraction: split on spaces and punctuation,
	// filter short words and common stop words
	stopWords := map[string]bool{
		"a": true, "an": true, "the": true, "and": true, "or": true,
		"is": true, "are": true, "was": true, "were": true, "be": true,
		"been": true, "being": true, "have": true, "has": true, "had": true,
		"do": true, "does": true, "did": true, "will": true, "would": true,
		"could": true, "should": true, "may": true, "might": true, "must": true,
		"can": true, "to": true, "of": true, "in": true, "for": true,
		"on": true, "with": true, "at": true, "by": true, "from": true,
		"as": true, "into": true, "through": true, "during": true,
		"before": true, "after": true, "above": true, "below": true,
		"between": true, "under": true, "again": true, "further": true,
		"then": true, "once": true, "here": true, "there": true, "when": true,
		"where": true, "why": true, "how": true, "all": true, "each": true,
		"few": true, "more": true, "most": true, "other": true, "some": true,
		"such": true, "no": true, "not": true, "only": true, "own": true,
		"same": true, "so": true, "than": true, "too": true, "very": true,
		"just": true, "use": true, "that": true, "this": true, "it": true,
	}

	var keywords []string
	scanner := bufio.NewScanner(strings.NewReader(description))
	scanner.Split(bufio.ScanWords)

	seen := make(map[string]bool)
	for scanner.Scan() {
		word := strings.ToLower(scanner.Text())
		// Remove punctuation
		word = strings.Trim(word, ".,;:!?()[]{}\"'")

		if len(word) < 3 || stopWords[word] || seen[word] {
			continue
		}
		seen[word] = true
		keywords = append(keywords, word)
	}

	return keywords
}

// SeedFactoryKnowledge seeds all Factory builtin knowledge into the store.
// This is idempotent - existing items are updated, not duplicated.
func SeedFactoryKnowledge(ctx context.Context, store knowledge.Store) (int, error) {
	droids, err := ListFactoryDroids()
	if err != nil {
		return 0, fmt.Errorf("list factory droids: %w", err)
	}

	seeded := 0
	for _, asset := range droids {
		// Upsert the knowledge item
		item, err := store.UpsertItem(ctx, knowledge.Item{
			Name:        asset.Name,
			Kind:        asset.Kind,
			Description: asset.Description,
			SourcePath:  asset.SourcePath,
			Priority:    "medium",
		})
		if err != nil {
			return seeded, fmt.Errorf("upsert item %s: %w", asset.Name, err)
		}

		// Clear existing triggers and add new ones
		if err := store.DeleteTriggersForItem(ctx, item.ID); err != nil {
			return seeded, fmt.Errorf("delete triggers for %s: %w", asset.Name, err)
		}

		// Add keyword triggers
		for _, kw := range asset.Keywords {
			_, err := store.AddTrigger(ctx, knowledge.Trigger{
				ItemID:      item.ID,
				TriggerKind: knowledge.TriggerKeyword,
				Pattern:     kw,
			})
			if err != nil {
				return seeded, fmt.Errorf("add trigger for %s: %w", asset.Name, err)
			}
		}

		// Also add "factory" and "droid" as standard triggers
		for _, tag := range []string{"factory", "droid"} {
			_, _ = store.AddTrigger(ctx, knowledge.Trigger{
				ItemID:      item.ID,
				TriggerKind: knowledge.TriggerKeyword,
				Pattern:     tag,
			})
		}

		// Clear existing documents and add the body
		if err := store.DeleteDocumentsForItem(ctx, item.ID); err != nil {
			return seeded, fmt.Errorf("delete documents for %s: %w", asset.Name, err)
		}

		_, err = store.UpsertDocument(ctx, knowledge.Document{
			ItemID:     item.ID,
			Title:      asset.Name,
			SourcePath: asset.SourcePath,
			Body:       asset.Body,
		})
		if err != nil {
			return seeded, fmt.Errorf("upsert document for %s: %w", asset.Name, err)
		}

		seeded++
	}

	return seeded, nil
}
