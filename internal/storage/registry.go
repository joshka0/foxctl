package storage

import "strings"

// StoreClass classifies a store by whether it should be synced across devices.
type StoreClass string

const (
	StoreClassSyncCritical  StoreClass = "sync_critical"
	StoreClassSyncUseful    StoreClass = "sync_useful"
	StoreClassLocalOnly     StoreClass = "local_only"
	StoreClassObservability StoreClass = "observability"
	StoreClassExternal      StoreClass = "external"
)

// StoreName is the canonical, env-var-friendly name for a store/database.
// These names are intended to match `AGENTCTL_<STORE>_...` environment variable prefixes.
type StoreName string

const (
	StoreCoordination         StoreName = "COORDINATION"
	StoreSessions             StoreName = "SESSIONS"
	StoreTasks                StoreName = "TASKS"
	StoreMailbox              StoreName = "MAILBOX"
	StoreAgents               StoreName = "AGENTS"
	StoreMemory               StoreName = "MEMORY"
	StoreCompanion            StoreName = "COMPANION"
	StoreContextVar           StoreName = "CONTEXTVAR"
	StoreKnowledge            StoreName = "KNOWLEDGE"
	StoreTeams                StoreName = "TEAMS"
	StoreTrajectory           StoreName = "TRAJECTORY"
	StoreBlackboard           StoreName = "BLACKBOARD"
	StoreBoard                StoreName = "BOARD"
	StoreCache                StoreName = "CACHE"
	StoreJobs                 StoreName = "JOBS"
	StoreQuotas               StoreName = "QUOTAS"
	StoreTestWatch            StoreName = "TESTWATCH"
	StoreContextBuffer        StoreName = "CONTEXTBUFFER"
	StoreGraph                StoreName = "GRAPH"
	StoreEmbeddingQueue       StoreName = "EMBEDDING_QUEUE"
	StoreSummaryQueue         StoreName = "SUMMARY_QUEUE"
	StoreDaemonDedupe         StoreName = "DAEMON_DEDUPE"
	StorePatterns             StoreName = "PATTERNS"
	StorePostReview           StoreName = "POST_REVIEW"
	StoreRepoIndex            StoreName = "REPOINDEX"
	StoreObsidianIndex        StoreName = "OBSIDIANINDEX"
	StoreConversationSettings StoreName = "CONVERSATION_SETTINGS"
	StoreCAS                  StoreName = "CAS"
	StoreEvents               StoreName = "EVENTS"
	StoreOpenCode             StoreName = "OPENCODE"
)

// StoreSpec describes one database-backed store used by foxctl.
type StoreSpec struct {
	Name StoreName
	// DefaultFile is the default filename (relative to the store's root dir)
	// or a human-readable pattern for dynamically named databases.
	DefaultFile string
	Class       StoreClass
	Notes       string
}

var canonicalStores = []StoreSpec{
	{Name: StoreCoordination, DefaultFile: "coordination.db", Class: StoreClassSyncCritical, Notes: "Daemon leader lease + coordination"},
	{Name: StoreSessions, DefaultFile: "sessions.db", Class: StoreClassSyncCritical, Notes: "Session history"},
	{Name: StoreTasks, DefaultFile: "tasks.db", Class: StoreClassSyncCritical, Notes: "Task continuity across devices"},
	{Name: StoreMailbox, DefaultFile: "mailbox.db", Class: StoreClassSyncCritical, Notes: "Agent messages"},
	{Name: StoreAgents, DefaultFile: "agents.db", Class: StoreClassSyncCritical, Notes: "Agent registry; also used by actor system registry (actor_registry)"},
	{Name: StoreMemory, DefaultFile: "memory.db", Class: StoreClassSyncCritical, Notes: "Semantic memory + indexer state"},
	{Name: StoreCompanion, DefaultFile: "companion.db", Class: StoreClassSyncCritical, Notes: "Companion conversation memory"},
	{Name: StoreContextVar, DefaultFile: "contextvar.db", Class: StoreClassSyncCritical, Notes: "RLM context store"},

	{Name: StoreKnowledge, DefaultFile: "knowledge.db", Class: StoreClassSyncUseful, Notes: "Extracted knowledge"},
	{Name: StoreTeams, DefaultFile: "teams.db", Class: StoreClassSyncUseful, Notes: "Team definitions"},
	{Name: StoreTrajectory, DefaultFile: "trajectory.db", Class: StoreClassSyncUseful, Notes: "Execution traces"},

	{Name: StoreBlackboard, DefaultFile: "blackboard.db", Class: StoreClassLocalOnly, Notes: "Local workspace coordination"},
	{Name: StoreBoard, DefaultFile: "board.db", Class: StoreClassLocalOnly, Notes: "Board state (local coordination)"},
	{Name: StoreCache, DefaultFile: "cache.db", Class: StoreClassLocalOnly, Notes: "Ephemeral speedup"},
	{Name: StoreJobs, DefaultFile: "jobs.db", Class: StoreClassLocalOnly, Notes: "Job queue state (local execution)"},
	{Name: StoreQuotas, DefaultFile: "quotas.db", Class: StoreClassLocalOnly, Notes: "Rate limiting (device-local)"},
	{Name: StoreTestWatch, DefaultFile: "test_watch.db", Class: StoreClassLocalOnly, Notes: "Test watcher state"},
	{Name: StoreContextBuffer, DefaultFile: "contextbuffer.db", Class: StoreClassLocalOnly, Notes: "Context buffer"},
	{Name: StoreGraph, DefaultFile: "graph.db", Class: StoreClassLocalOnly, Notes: "Code relationship graph (rebuildable)"},
	{Name: StoreEmbeddingQueue, DefaultFile: "embedding_queue.db", Class: StoreClassLocalOnly, Notes: "Embedding job queue"},
	{Name: StoreSummaryQueue, DefaultFile: "summary_queue.db", Class: StoreClassLocalOnly, Notes: "Session summary job queue"},
	{Name: StoreDaemonDedupe, DefaultFile: "daemon_dedupe.db", Class: StoreClassLocalOnly, Notes: "Message deduplication (single-machine)"},
	{Name: StorePatterns, DefaultFile: "patterns.db", Class: StoreClassLocalOnly, Notes: "Agent optimization patterns"},
	{Name: StorePostReview, DefaultFile: "post_review_events.db", Class: StoreClassLocalOnly, Notes: "Post-review event tracking"},
	{Name: StoreConversationSettings, DefaultFile: "conversation_settings.db", Class: StoreClassLocalOnly, Notes: "Per-conversation settings overrides"},
	{Name: StoreRepoIndex, DefaultFile: "repoindex/<key>.db", Class: StoreClassLocalOnly, Notes: "Per-repo code index (dynamic name)"},
	{Name: StoreObsidianIndex, DefaultFile: "obsidianindex-<key>.db", Class: StoreClassLocalOnly, Notes: "Per-vault Obsidian note index (dynamic name)"},
	{Name: StoreCAS, DefaultFile: "cas.db", Class: StoreClassLocalOnly, Notes: "CAS metadata"},

	{Name: StoreEvents, DefaultFile: "events.db", Class: StoreClassObservability, Notes: "Stored under AGENTCTL_OBS_DIR"},
	{Name: StoreOpenCode, DefaultFile: "opencode.db", Class: StoreClassExternal, Notes: "External import (~/.opencode or workspace .opencode)"},
}

var canonicalStoreByName = func() map[string]StoreSpec {
	out := make(map[string]StoreSpec, len(canonicalStores))
	for _, spec := range canonicalStores {
		out[string(spec.Name)] = spec
	}
	return out
}()

// CanonicalStores returns the canonical registry of foxctl-managed stores.
//
// Index:
// - Purpose: Provide a single source of truth for store names, default DB filenames, and sync classification
// - Flow: copy static registry slice → return copy
// - SideEffects: none
// - FailureModes: none
// - Related: FindStore, StoreSpec, StoreName, StoreClass
// - Keywords: store_registry, db_files, sync_classification
func CanonicalStores() []StoreSpec {
	out := make([]StoreSpec, len(canonicalStores))
	copy(out, canonicalStores)
	return out
}

// FindStore returns the store spec for name, if present.
//
// Index:
// - Purpose: Resolve a store name into its canonical spec
// - Flow: normalize name → map lookup → return spec
// - SideEffects: none
// - FailureModes: none
// - Related: CanonicalStores
// - Keywords: store_lookup, canonical_store
func FindStore(name string) (StoreSpec, bool) {
	key := strings.ToUpper(strings.TrimSpace(name))
	spec, ok := canonicalStoreByName[key]
	return spec, ok
}
