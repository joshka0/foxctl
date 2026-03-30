package transcriptpipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/sessionkit/claudejsonl"
	"github.com/jkatigb/agentctl/internal/sessionkit/codexjsonl"
	"github.com/jkatigb/agentctl/internal/storage/transcriptcache"
	"github.com/jkatigb/agentctl/internal/v2/adapters/sourceimport"
)

// OpenTranscriptCacheStore opens the shared transcript cache under the configured
// storage root, falling back to the historical Codex cache path when needed.
func OpenTranscriptCacheStore(ctx context.Context, storageRoot string) (*transcriptcache.Store, string, error) {
	homeDir := ""
	if home, err := os.UserHomeDir(); err == nil {
		homeDir = home
	}
	candidates := transcriptCacheRoots(storageRoot, homeDir)

	var errs []string
	for _, root := range candidates {
		if err := os.MkdirAll(root, 0o755); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", root, err))
			continue
		}
		store, err := transcriptcache.Open(ctx, root)
		if err == nil {
			return store, root, nil
		}
		errs = append(errs, fmt.Sprintf("%s: %v", root, err))
	}
	return nil, "", fmt.Errorf("open transcript cache store: %s", strings.Join(errs, " | "))
}

// ResolveAndParseTranscript resolves one source session path and parses it.
func ResolveAndParseTranscript(provider, sourceFile, sessionID, workspace, actorID string) (sourceimport.ParsedSession, error) {
	resolvedProvider := sourceimport.Provider(strings.ToLower(strings.TrimSpace(provider)))
	if resolvedProvider == "" {
		resolvedProvider = sourceimport.ProviderAuto
	}
	switch resolvedProvider {
	case sourceimport.ProviderAuto, sourceimport.ProviderClaude, sourceimport.ProviderCodex:
	default:
		return sourceimport.ParsedSession{}, fmt.Errorf("--provider must be one of: auto, claude, codex")
	}

	resolvedSourcePath := strings.TrimSpace(sourceFile)
	resolvedSessionID := strings.TrimSpace(sessionID)
	resolvedWorkspace := strings.TrimSpace(workspace)
	if resolvedWorkspace == "" {
		resolvedWorkspace = strings.TrimSpace(os.Getenv("AGENTCTL_WORKSPACE"))
	}
	if resolvedSourcePath != "" && resolvedProvider == sourceimport.ProviderAuto {
		detected, err := sourceimport.DetectProviderFromFile(resolvedSourcePath)
		if err != nil {
			return sourceimport.ParsedSession{}, err
		}
		resolvedProvider = detected
	}

	if resolvedSourcePath == "" {
		switch resolvedProvider {
		case sourceimport.ProviderClaude:
			if resolvedSessionID == "" {
				return sourceimport.ParsedSession{}, fmt.Errorf("--session-id is required for --provider claude when --source-file is unset")
			}
			resolvedSourcePath = claudejsonl.LocateSessionJSONL(resolvedWorkspace, resolvedSessionID)
		case sourceimport.ProviderCodex:
			if resolvedSessionID != "" {
				resolvedSourcePath = codexjsonl.LocateSessionJSONL(resolvedSessionID)
			} else {
				path, sid := codexjsonl.LocateMostRecentSessionJSONL()
				resolvedSourcePath = strings.TrimSpace(path)
				resolvedSessionID = firstNonEmpty(resolvedSessionID, strings.TrimSpace(sid))
			}
		case sourceimport.ProviderAuto:
			if resolvedSessionID != "" {
				if path := claudejsonl.LocateSessionJSONL(resolvedWorkspace, resolvedSessionID); path != "" {
					resolvedSourcePath = path
					resolvedProvider = sourceimport.ProviderClaude
				} else if path := codexjsonl.LocateSessionJSONL(resolvedSessionID); path != "" {
					resolvedSourcePath = path
					resolvedProvider = sourceimport.ProviderCodex
				}
			}
			if resolvedSourcePath == "" {
				if path, sid := codexjsonl.LocateMostRecentSessionJSONL(); path != "" {
					resolvedSourcePath = strings.TrimSpace(path)
					resolvedProvider = sourceimport.ProviderCodex
					resolvedSessionID = firstNonEmpty(resolvedSessionID, strings.TrimSpace(sid))
				}
			}
		}
	}
	if strings.TrimSpace(resolvedSourcePath) == "" {
		return sourceimport.ParsedSession{}, fmt.Errorf("source session JSONL could not be resolved")
	}
	if resolvedWorkspace == "" && strings.TrimSpace(sourceFile) == "" {
		resolvedWorkspace = ws.Detect("")
	}

	switch resolvedProvider {
	case sourceimport.ProviderClaude:
		parsed, err := sourceimport.ParseClaudeFile(resolvedSourcePath, resolvedSessionID, resolvedWorkspace, actorID)
		if err != nil {
			return sourceimport.ParsedSession{}, err
		}
		return backfillParsedSessionWorkspace(parsed, resolvedSourcePath, resolvedWorkspace), nil
	case sourceimport.ProviderCodex:
		parsed, err := sourceimport.ParseCodexFile(resolvedSourcePath, resolvedSessionID, resolvedWorkspace, actorID)
		if err != nil {
			return sourceimport.ParsedSession{}, err
		}
		return backfillParsedSessionWorkspace(parsed, resolvedSourcePath, resolvedWorkspace), nil
	default:
		return sourceimport.ParsedSession{}, fmt.Errorf("provider could not be resolved")
	}
}

// LoadSourceBundles parses transcript files and enriches them with source metadata.
func LoadSourceBundles(sourceFiles []string, actorID, workspaceHint string) ([]SourceBundle, error) {
	bundles := make([]SourceBundle, 0, len(sourceFiles))
	for _, path := range sourceFiles {
		parsed, err := ResolveAndParseTranscript("auto", strings.TrimSpace(path), "", workspaceHint, actorID)
		if err != nil {
			return nil, err
		}
		meta, err := InspectSource(path, parsed, workspaceHint)
		if err != nil {
			return nil, err
		}
		bundles = append(bundles, SourceBundle{
			Meta:   meta,
			Parsed: parsed,
		})
	}
	return bundles, nil
}

func transcriptCacheRoots(storageRoot, homeDir string) []string {
	candidates := make([]string, 0, 2)
	if root := strings.TrimSpace(storageRoot); root != "" {
		candidates = append(candidates, root)
	}
	if home := strings.TrimSpace(homeDir); home != "" {
		candidates = append(candidates, filepath.Join(home, ".codex", "memories", "agentctl-transcript-cache"))
	}
	return candidates
}

func backfillParsedSessionWorkspace(parsed sourceimport.ParsedSession, sourcePath, workspaceHint string) sourceimport.ParsedSession {
	parsed.WorkspacePath = strings.TrimSpace(parsed.WorkspacePath)
	if parsed.WorkspacePath != "" {
		return parsed
	}
	meta, err := InspectSource(sourcePath, parsed, workspaceHint)
	if err == nil && strings.TrimSpace(meta.WorkspacePath) != "" {
		parsed.WorkspacePath = strings.TrimSpace(meta.WorkspacePath)
		return parsed
	}
	parsed.WorkspacePath = strings.TrimSpace(workspaceHint)
	return parsed
}
