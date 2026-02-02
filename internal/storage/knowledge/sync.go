package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// SyncResult contains statistics from a sync operation.
type SyncResult struct {
	PacksAdded      int      `json:"packs_added"`
	PacksUpdated    int      `json:"packs_updated"`
	AgentsAdded     int      `json:"agents_added"`
	AgentsUpdated   int      `json:"agents_updated"`
	CommandsAdded   int      `json:"commands_added"`
	CommandsUpdated int      `json:"commands_updated"`
	TriggersAdded   int      `json:"triggers_added"`
	DocumentsAdded  int      `json:"documents_added"`
	Errors          []string `json:"errors,omitempty"`
}

// SyncOptions configures the sync behavior.
type SyncOptions struct {
	WorkspaceRoot string // Root directory to scan
	KnowledgeDir  string // Relative path to knowledge packs (default: docs/knowledge)
	AgentsDir     string // Relative path to agents (default: .claude/agents)
	CommandsDir   string // Relative path to commands (default: .claude/commands)
}

// DefaultSyncOptions returns default sync options.
func DefaultSyncOptions(workspaceRoot string) SyncOptions {
	return SyncOptions{
		WorkspaceRoot: workspaceRoot,
		KnowledgeDir:  "docs/knowledge",
		AgentsDir:     ".claude/agents",
		CommandsDir:   ".claude/commands",
	}
}

// Sync walks the filesystem and populates the knowledge store.
func Sync(ctx context.Context, store Store, opts SyncOptions) (*SyncResult, error) {
	result := &SyncResult{}

	// Sync knowledge packs
	if err := syncKnowledgePacks(ctx, store, opts, result); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("packs: %v", err))
	}

	// Sync agents
	if err := syncAgents(ctx, store, opts, result); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("agents: %v", err))
	}

	// Sync commands
	if err := syncCommands(ctx, store, opts, result); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("commands: %v", err))
	}

	return result, nil
}

func syncKnowledgePacks(ctx context.Context, store Store, opts SyncOptions, result *SyncResult) error {
	knowledgeDir := filepath.Join(opts.WorkspaceRoot, opts.KnowledgeDir)
	if _, err := os.Stat(knowledgeDir); os.IsNotExist(err) {
		return nil // No knowledge directory, skip
	}

	// Load skill-rules.json if it exists
	rulesPath := filepath.Join(knowledgeDir, "skill-rules.json")
	rules, err := loadSkillRules(rulesPath)
	if err != nil && !os.IsNotExist(err) {
		result.Errors = append(result.Errors, fmt.Sprintf("load skill-rules.json: %v", err))
	}

	// Walk knowledge directory
	entries, err := os.ReadDir(knowledgeDir)
	if err != nil {
		return fmt.Errorf("read knowledge dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue // Skip files at root level (like skill-rules.json, README.md)
		}

		packName := entry.Name()
		packDir := filepath.Join(knowledgeDir, packName)

		// Find main doc (SKILL.md or README.md)
		mainDoc, mainDocPath := findMainDoc(packDir)
		if mainDoc == "" {
			continue // No main doc, skip
		}

		// Parse frontmatter for description
		content, err := os.ReadFile(mainDocPath)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("read %s: %v", mainDocPath, err))
			continue
		}

		fm := parseFrontmatter(string(content))
		description := fm["description"]
		if description == "" {
			description = fmt.Sprintf("Knowledge pack: %s", packName)
		}

		// Check if item exists
		existing, found, err := store.GetItemByName(ctx, packName)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("get item %s: %v", packName, err))
			continue
		}

		// Upsert item
		item := Item{
			Name:        packName,
			Kind:        KindPack,
			Description: description,
			SourcePath:  filepath.Join(opts.KnowledgeDir, packName),
			Priority:    "medium",
		}
		if found {
			item.ID = existing.ID
			item.CreatedAt = existing.CreatedAt
		}

		item, err = store.UpsertItem(ctx, item)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("upsert item %s: %v", packName, err))
			continue
		}

		if found {
			result.PacksUpdated++
		} else {
			result.PacksAdded++
		}

		// Clear and re-add triggers
		if err := store.DeleteTriggersForItem(ctx, item.ID); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("delete triggers for %s: %v", packName, err))
		}

		// Add triggers from skill-rules.json
		if rule, ok := rules[packName]; ok {
			triggers := extractTriggers(rule)
			for _, t := range triggers {
				t.ItemID = item.ID
				if _, err := store.AddTrigger(ctx, t); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("add trigger for %s: %v", packName, err))
				} else {
					result.TriggersAdded++
				}
			}
		}

		// Clear and re-add documents
		if err := store.DeleteDocumentsForItem(ctx, item.ID); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("delete docs for %s: %v", packName, err))
		}

		// Add main document
		doc := Document{
			ItemID:     item.ID,
			Title:      packName,
			SourcePath: mainDocPath,
			Body:       string(content),
		}
		if _, err := store.UpsertDocument(ctx, doc); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("add doc for %s: %v", packName, err))
		} else {
			result.DocumentsAdded++
		}

		// Add resource documents
		resourcesDir := filepath.Join(packDir, "resources")
		if _, err := os.Stat(resourcesDir); err == nil {
			err := filepath.WalkDir(resourcesDir, func(path string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				if !strings.HasSuffix(path, ".md") {
					return nil
				}
				content, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				doc := Document{
					ItemID:     item.ID,
					Title:      strings.TrimSuffix(d.Name(), ".md"),
					SourcePath: path,
					Body:       string(content),
				}
				if _, err := store.UpsertDocument(ctx, doc); err != nil {
					return err
				}
				result.DocumentsAdded++
				return nil
			})
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("walk resources for %s: %v", packName, err))
			}
		}
	}

	return nil
}

func syncAgents(ctx context.Context, store Store, opts SyncOptions, result *SyncResult) error {
	agentsDir := filepath.Join(opts.WorkspaceRoot, opts.AgentsDir)
	if _, err := os.Stat(agentsDir); os.IsNotExist(err) {
		return nil
	}

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return fmt.Errorf("read agents dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		if entry.Name() == "README.md" {
			continue
		}

		agentName := strings.TrimSuffix(entry.Name(), ".md")
		agentPath := filepath.Join(agentsDir, entry.Name())

		content, err := os.ReadFile(agentPath)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("read agent %s: %v", agentName, err))
			continue
		}

		fm := parseFrontmatter(string(content))
		description := fm["description"]
		if description == "" {
			description = fmt.Sprintf("Agent: %s", agentName)
		}

		existing, found, err := store.GetItemByName(ctx, agentName)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("get agent %s: %v", agentName, err))
			continue
		}

		item := Item{
			Name:        agentName,
			Kind:        KindAgent,
			Description: description,
			SourcePath:  filepath.Join(opts.AgentsDir, entry.Name()),
			Priority:    "medium",
		}
		if found {
			item.ID = existing.ID
			item.CreatedAt = existing.CreatedAt
		}

		item, err = store.UpsertItem(ctx, item)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("upsert agent %s: %v", agentName, err))
			continue
		}

		if found {
			result.AgentsUpdated++
		} else {
			result.AgentsAdded++
		}

		// Extract keywords from description for triggers
		if err := store.DeleteTriggersForItem(ctx, item.ID); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("delete triggers for agent %s: %v", agentName, err))
		}

		keywords := extractKeywordsFromDescription(description)
		for _, kw := range keywords {
			t := Trigger{
				ItemID:      item.ID,
				TriggerKind: TriggerKeyword,
				Pattern:     kw,
			}
			if _, err := store.AddTrigger(ctx, t); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("add trigger for agent %s: %v", agentName, err))
			} else {
				result.TriggersAdded++
			}
		}

		// Add document
		if err := store.DeleteDocumentsForItem(ctx, item.ID); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("delete docs for agent %s: %v", agentName, err))
		}

		doc := Document{
			ItemID:     item.ID,
			Title:      agentName,
			SourcePath: agentPath,
			Body:       string(content),
		}
		if _, err := store.UpsertDocument(ctx, doc); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("add doc for agent %s: %v", agentName, err))
		} else {
			result.DocumentsAdded++
		}
	}

	return nil
}

func syncCommands(ctx context.Context, store Store, opts SyncOptions, result *SyncResult) error {
	commandsDir := filepath.Join(opts.WorkspaceRoot, opts.CommandsDir)
	if _, err := os.Stat(commandsDir); os.IsNotExist(err) {
		return nil
	}

	entries, err := os.ReadDir(commandsDir)
	if err != nil {
		return fmt.Errorf("read commands dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		cmdName := strings.TrimSuffix(entry.Name(), ".md")
		cmdPath := filepath.Join(commandsDir, entry.Name())

		content, err := os.ReadFile(cmdPath)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("read command %s: %v", cmdName, err))
			continue
		}

		fm := parseFrontmatter(string(content))
		description := fm["description"]
		if description == "" {
			description = fmt.Sprintf("Command: %s", cmdName)
		}

		existing, found, err := store.GetItemByName(ctx, cmdName)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("get command %s: %v", cmdName, err))
			continue
		}

		item := Item{
			Name:        cmdName,
			Kind:        KindCommand,
			Description: description,
			SourcePath:  filepath.Join(opts.CommandsDir, entry.Name()),
			Priority:    "medium",
		}
		if found {
			item.ID = existing.ID
			item.CreatedAt = existing.CreatedAt
		}

		item, err = store.UpsertItem(ctx, item)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("upsert command %s: %v", cmdName, err))
			continue
		}

		if found {
			result.CommandsUpdated++
		} else {
			result.CommandsAdded++
		}

		// Clear triggers and docs
		if err := store.DeleteTriggersForItem(ctx, item.ID); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("delete triggers for command %s: %v", cmdName, err))
		}
		if err := store.DeleteDocumentsForItem(ctx, item.ID); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("delete docs for command %s: %v", cmdName, err))
		}

		// Add document
		doc := Document{
			ItemID:     item.ID,
			Title:      cmdName,
			SourcePath: cmdPath,
			Body:       string(content),
		}
		if _, err := store.UpsertDocument(ctx, doc); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("add doc for command %s: %v", cmdName, err))
		} else {
			result.DocumentsAdded++
		}
	}

	return nil
}

// Helper functions

func findMainDoc(dir string) (name, path string) {
	for _, candidate := range []string{"SKILL.md", "README.md"} {
		p := filepath.Join(dir, candidate)
		if _, err := os.Stat(p); err == nil {
			return candidate, p
		}
	}
	return "", ""
}

func parseFrontmatter(content string) map[string]string {
	result := make(map[string]string)
	if !strings.HasPrefix(content, "---") {
		return result
	}

	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		return result
	}

	var fm map[string]any
	if err := yaml.Unmarshal([]byte(parts[1]), &fm); err != nil {
		return result
	}

	for k, v := range fm {
		if s, ok := v.(string); ok {
			result[k] = s
		}
	}
	return result
}

// SkillRule represents a rule from skill-rules.json
type SkillRule struct {
	Type           string `json:"type"`
	Enforcement    string `json:"enforcement"`
	Priority       string `json:"priority"`
	Description    string `json:"description"`
	PromptTriggers struct {
		Keywords       []string `json:"keywords"`
		IntentPatterns []string `json:"intentPatterns"`
	} `json:"promptTriggers"`
	FileTriggers struct {
		PathPatterns    []string `json:"pathPatterns"`
		PathExclusions  []string `json:"pathExclusions"`
		ContentPatterns []string `json:"contentPatterns"`
	} `json:"fileTriggers"`
}

func loadSkillRules(path string) (map[string]SkillRule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Skills map[string]SkillRule `json:"skills"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	return raw.Skills, nil
}

func extractTriggers(rule SkillRule) []Trigger {
	var triggers []Trigger

	// Keywords
	for _, kw := range rule.PromptTriggers.Keywords {
		triggers = append(triggers, Trigger{
			TriggerKind: TriggerKeyword,
			Pattern:     strings.ToLower(kw),
		})
	}

	// Intent patterns
	for _, pattern := range rule.PromptTriggers.IntentPatterns {
		triggers = append(triggers, Trigger{
			TriggerKind: TriggerIntent,
			Pattern:     pattern,
		})
	}

	// Path patterns
	for _, pattern := range rule.FileTriggers.PathPatterns {
		triggers = append(triggers, Trigger{
			TriggerKind: TriggerPath,
			Pattern:     pattern,
		})
	}

	// Content patterns
	for _, pattern := range rule.FileTriggers.ContentPatterns {
		triggers = append(triggers, Trigger{
			TriggerKind: TriggerContent,
			Pattern:     pattern,
		})
	}

	return triggers
}

var wordRe = regexp.MustCompile(`\b[a-zA-Z][a-zA-Z0-9_-]{2,}\b`)

func extractKeywordsFromDescription(desc string) []string {
	// Extract meaningful words from description
	words := wordRe.FindAllString(strings.ToLower(desc), -1)

	// Filter out common words
	stopWords := map[string]bool{
		"the": true, "and": true, "for": true, "this": true, "that": true,
		"with": true, "from": true, "use": true, "when": true, "you": true,
		"are": true, "have": true, "has": true, "will": true, "can": true,
		"agent": true, "command": true, "skill": true,
	}

	var keywords []string
	seen := make(map[string]bool)
	for _, w := range words {
		if !stopWords[w] && !seen[w] && len(w) > 3 {
			keywords = append(keywords, w)
			seen[w] = true
		}
	}

	// Limit to first 10 keywords
	if len(keywords) > 10 {
		keywords = keywords[:10]
	}

	return keywords
}
