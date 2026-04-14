package builtin

import (
	"bufio"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/joshka0/foxctl/internal/storage/knowledge"
)

//go:embed data/droids/*.md
var factoryDroids embed.FS

//go:embed data/agents/*.md
var coreAgents embed.FS

// Asset represents a single embedded knowledge asset.
type Asset struct {
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
func ListFactoryDroids() ([]Asset, error) {
	return listEmbeddedAssets(factoryDroids, "data/droids", "factory/droid", "builtin://factory/droids")
}

// ListCoreAgents returns all embedded core agent assets.
func ListCoreAgents() ([]Asset, error) {
	return listEmbeddedAssets(coreAgents, "data/agents", "core/agent", "builtin://core/agents")
}

// listEmbeddedAssets walks an embedded FS and parses all markdown files.
// Note: Context cancellation is not checked here since embedded FS walks are
// very fast (typically <10 files). If this changes, add ctx parameter and check
// ctx.Done() in the walk callback.
func listEmbeddedAssets(fsys embed.FS, dir, namePrefix, sourcePrefix string) ([]Asset, error) {
	var assets []Asset

	err := fs.WalkDir(fsys, dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}

		content, err := fsys.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read embedded file %s: %w", path, err)
		}

		asset, err := parseAgentAsset(path, string(content), dir, namePrefix, sourcePrefix)
		if err != nil {
			return fmt.Errorf("parse asset %s: %w", path, err)
		}

		assets = append(assets, asset)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return assets, nil
}

// parseAgentAsset parses an agent/droid markdown file and extracts metadata.
func parseAgentAsset(path, content, dir, namePrefix, sourcePrefix string) (Asset, error) {
	// Extract filename without extension for slug
	filename := strings.TrimPrefix(path, dir+"/")
	slug := strings.TrimSuffix(filename, ".md")

	// Parse YAML frontmatter
	_, description := parseYAMLFrontmatter(content)

	// Generate keywords from description
	keywords := extractKeywords(description)

	return Asset{
		Name:        fmt.Sprintf("%s/%s", namePrefix, slug),
		Kind:        knowledge.KindAgent,
		Description: description,
		SourcePath:  fmt.Sprintf("%s/%s.md", sourcePrefix, slug),
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

// SeedBuiltinKnowledge seeds all builtin knowledge (Factory droids + core agents) into the store.
// This is idempotent - existing items are updated, not duplicated.
// Errors during seeding are aggregated rather than failing fast, allowing partial success.
func SeedBuiltinKnowledge(ctx context.Context, store knowledge.Store) (int, error) {
	var allAssets []Asset

	// Collect Factory droids
	droids, err := ListFactoryDroids()
	if err != nil {
		return 0, fmt.Errorf("list factory droids: %w", err)
	}
	allAssets = append(allAssets, droids...)

	// Collect core agents
	agents, err := ListCoreAgents()
	if err != nil {
		return 0, fmt.Errorf("list core agents: %w", err)
	}
	allAssets = append(allAssets, agents...)

	seeded := 0
	var seedErrors []string
	for _, asset := range allAssets {
		if err := seedAsset(ctx, store, asset); err != nil {
			seedErrors = append(seedErrors, fmt.Sprintf("%s: %v", asset.Name, err))
			continue
		}
		seeded++
	}

	if len(seedErrors) > 0 {
		return seeded, fmt.Errorf("seeded %d/%d assets with %d errors: %v", seeded, len(allAssets), len(seedErrors), seedErrors)
	}
	return seeded, nil
}

// SeedFactoryKnowledge seeds only Factory droids (for backwards compatibility).
// Errors during seeding are aggregated rather than failing fast, allowing partial success.
func SeedFactoryKnowledge(ctx context.Context, store knowledge.Store) (int, error) {
	droids, err := ListFactoryDroids()
	if err != nil {
		return 0, fmt.Errorf("list factory droids: %w", err)
	}

	seeded := 0
	var seedErrors []string
	for _, asset := range droids {
		if err := seedAsset(ctx, store, asset); err != nil {
			seedErrors = append(seedErrors, fmt.Sprintf("%s: %v", asset.Name, err))
			continue
		}
		seeded++
	}

	if len(seedErrors) > 0 {
		return seeded, fmt.Errorf("seeded %d/%d droids with %d errors: %v", seeded, len(droids), len(seedErrors), seedErrors)
	}
	return seeded, nil
}

// seedAsset seeds a single asset into the store.
// Note: This function is NOT safe for concurrent calls with the same asset.Name.
// Callers must ensure sequential seeding or implement their own locking.
// SQLite WAL mode provides some protection but not full atomicity.
func seedAsset(ctx context.Context, store knowledge.Store, asset Asset) error {
	// Upsert the knowledge item
	item, err := store.UpsertItem(ctx, knowledge.Item{
		Name:        asset.Name,
		Kind:        asset.Kind,
		Description: asset.Description,
		SourcePath:  asset.SourcePath,
		Priority:    "medium",
	})
	if err != nil {
		return fmt.Errorf("upsert item %s: %w", asset.Name, err)
	}

	// Clear existing triggers and add new ones
	if err := store.DeleteTriggersForItem(ctx, item.ID); err != nil {
		return fmt.Errorf("delete triggers for %s: %w", asset.Name, err)
	}

	// Add keyword triggers
	for _, kw := range asset.Keywords {
		_, err := store.AddTrigger(ctx, knowledge.Trigger{
			ItemID:      item.ID,
			TriggerKind: knowledge.TriggerKeyword,
			Pattern:     kw,
		})
		if err != nil {
			return fmt.Errorf("add trigger for %s: %w", asset.Name, err)
		}
	}

	// Add category tags based on name prefix
	var tags []string
	if strings.HasPrefix(asset.Name, "factory/droid/") {
		tags = []string{"factory", "droid"}
	} else if strings.HasPrefix(asset.Name, "core/agent/") {
		tags = []string{"core", "agent"}
	}
	for _, tag := range tags {
		// Tag triggers are best-effort; don't fail seeding if they can't be added
		if _, err := store.AddTrigger(ctx, knowledge.Trigger{
			ItemID:      item.ID,
			TriggerKind: knowledge.TriggerKeyword,
			Pattern:     tag,
		}); err != nil {
			// Log but continue - tags are supplementary to primary keywords
			_ = err // Acknowledged: tag trigger creation is best-effort
		}
	}

	// Clear existing documents and add the body
	if err := store.DeleteDocumentsForItem(ctx, item.ID); err != nil {
		return fmt.Errorf("delete documents for %s: %w", asset.Name, err)
	}

	_, err = store.UpsertDocument(ctx, knowledge.Document{
		ItemID:     item.ID,
		Title:      asset.Name,
		SourcePath: asset.SourcePath,
		Body:       asset.Body,
	})
	if err != nil {
		return fmt.Errorf("upsert document for %s: %w", asset.Name, err)
	}

	return nil
}
