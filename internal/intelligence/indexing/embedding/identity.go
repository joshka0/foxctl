package embedding

import (
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedqueue"
	platformsymbol "github.com/joshka0/foxctl/internal/platform/symbolutil"
)

type symbolQueueIdentity struct {
	SymbolID   string
	PackageID  string
	SymbolKey  string
	MemoryName string
}

func dedupeKeyForSymbol(workspaceID string, sym SymbolInput, contentDigest, model string) string {
	return embedqueue.StableDedupeKey(
		string(embedqueue.TaskKindSymbol),
		workspaceID,
		symbolDedupeIdentity(sym),
		model,
		contentDigest,
	)
}

func symbolInputWithCanonicalIdentity(sym SymbolInput, identity symbolQueueIdentity) SymbolInput {
	sym.SymbolID = identity.SymbolID
	sym.PackageID = identity.PackageID
	sym.SymbolKey = identity.SymbolKey
	sym.MemoryName = identity.MemoryName
	return sym
}

func resolveSymbolQueueIdentity(workspaceID string, sym SymbolInput) (symbolQueueIdentity, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return symbolQueueIdentity{}, fmt.Errorf("workspace_id is required")
	}

	memoryName := strings.TrimSpace(sym.MemoryName)
	packageID := strings.TrimSpace(sym.PackageID)
	symbolKey := strings.TrimSpace(sym.SymbolKey)

	if packageID != "" || symbolKey != "" {
		if packageID == "" || symbolKey == "" {
			return symbolQueueIdentity{}, fmt.Errorf("package_id and symbol_key are both required when either is set")
		}
		expectedName := platformsymbol.KeyEntryName(workspaceID, packageID, symbolKey)
		if memoryName != "" && memoryName != expectedName {
			return symbolQueueIdentity{}, fmt.Errorf("memory_name %q does not match package_id and symbol_key", memoryName)
		}
		return symbolQueueIdentity{
			SymbolID:   platformsymbol.ScopedSymbolID(packageID, symbolKey),
			PackageID:  packageID,
			SymbolKey:  symbolKey,
			MemoryName: expectedName,
		}, nil
	}

	if memoryName != "" {
		identity, ok := identityFromMemoryName(workspaceID, memoryName)
		if !ok {
			return symbolQueueIdentity{}, fmt.Errorf("memory_name must be a canonical symbol memory name")
		}
		return identity, nil
	}

	return symbolQueueIdentity{}, fmt.Errorf("symbol embedding identity requires memory_name or package_id and symbol_key")
}

func legacySymbolQueueIdentity(workspaceID, rawSymbolID string) (symbolQueueIdentity, bool) {
	workspaceID = strings.TrimSpace(workspaceID)
	rawSymbolID = strings.TrimSpace(rawSymbolID)
	if workspaceID == "" || rawSymbolID == "" {
		return symbolQueueIdentity{}, false
	}
	if identity, ok := identityFromMemoryName(workspaceID, rawSymbolID); ok {
		return identity, true
	}
	packageID, symbolKey, ok := splitScopedSymbolID(rawSymbolID)
	if !ok {
		return symbolQueueIdentity{}, false
	}
	return symbolQueueIdentity{
		SymbolID:   rawSymbolID,
		PackageID:  packageID,
		SymbolKey:  symbolKey,
		MemoryName: platformsymbol.KeyEntryName(workspaceID, packageID, symbolKey),
	}, true
}

func identityFromMemoryName(workspaceID, memoryName string) (symbolQueueIdentity, bool) {
	scopedID, ok := platformsymbol.ScopedSymbolIDFromKeyEntryName(workspaceID, memoryName)
	if !ok {
		return symbolQueueIdentity{}, false
	}
	packageID, symbolKey, ok := splitScopedSymbolID(scopedID)
	if !ok {
		return symbolQueueIdentity{}, false
	}
	return symbolQueueIdentity{
		SymbolID:   scopedID,
		PackageID:  packageID,
		SymbolKey:  symbolKey,
		MemoryName: strings.TrimSpace(memoryName),
	}, true
}

func splitScopedSymbolID(scopedID string) (string, string, bool) {
	parts := strings.SplitN(strings.TrimSpace(scopedID), "::", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

func validateStoredPayloadIdentity(payload embeddingPayload) error {
	switch payload.Kind {
	case embedqueue.TaskKindMemory:
		if strings.TrimSpace(payload.MemoryName) == "" {
			return fmt.Errorf("memory embedding job missing memory_name")
		}
		return nil
	case embedqueue.TaskKindSymbol:
		if _, err := resolveSymbolQueueIdentity(payload.WorkspaceID, SymbolInput{
			PackageID:  payload.PackageID,
			SymbolKey:  payload.SymbolKey,
			MemoryName: payload.MemoryName,
		}); err != nil {
			return fmt.Errorf("invalid symbol embedding identity: %w", err)
		}
		return nil
	default:
		return nil
	}
}

func symbolDedupeIdentity(sym SymbolInput) string {
	if name := strings.TrimSpace(sym.MemoryName); name != "" {
		return name
	}
	language := strings.TrimSpace(sym.Language)
	packageID := strings.TrimSpace(sym.PackageID)
	symbolKey := strings.TrimSpace(sym.SymbolKey)
	if packageID != "" && symbolKey != "" {
		return strings.Join([]string{language, packageID, symbolKey}, "\x00")
	}
	return strings.TrimSpace(sym.SymbolID)
}

func dedupeKeyForMemory(workspaceID, name, contentDigest, model string) string {
	return embedqueue.StableDedupeKey(
		string(embedqueue.TaskKindMemory),
		workspaceID,
		name,
		model,
		contentDigest,
	)
}
