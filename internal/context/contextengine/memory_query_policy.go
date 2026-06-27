package contextengine

import "strings"

// MemoryQueryScope describes task/session scoping for memory claim retrieval.
type MemoryQueryScope struct {
	TaskID    string
	SessionID string
}

// HasScope reports whether the query is constrained to a task or session.
func (s MemoryQueryScope) HasScope() bool {
	return strings.TrimSpace(s.TaskID) != "" || strings.TrimSpace(s.SessionID) != ""
}

// EffectiveMemoryQueryStatuses applies the default lifecycle visibility policy.
// Scoped retrieval can see in-flight claims; unscoped retrieval only sees
// reviewed current claims unless the caller supplied an explicit status set.
func EffectiveMemoryQueryStatuses(statuses []ClaimStatus, scope MemoryQueryScope) []ClaimStatus {
	if len(statuses) > 0 {
		return statuses
	}
	if scope.HasScope() {
		return []ClaimStatus{
			ClaimStatusCurrent,
			ClaimStatusCandidate,
			ClaimStatusNeedsRevalidation,
		}
	}
	return []ClaimStatus{ClaimStatusCurrent}
}

// MemoryQueryAllowsNamedFallback reports whether legacy named-memory recall is
// eligible for a claim lifecycle query. Named memory represents current facts.
func MemoryQueryAllowsNamedFallback(statuses []ClaimStatus) bool {
	for _, status := range statuses {
		if status == ClaimStatusCurrent {
			return true
		}
	}
	return false
}

// ClaimMatchesMemoryQuery reports whether query text appears in searchable
// claim fields.
func ClaimMatchesMemoryQuery(claim MemoryClaim, query string) bool {
	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		return true
	}
	if strings.Contains(strings.ToLower(claim.Summary), q) {
		return true
	}
	if strings.Contains(strings.ToLower(claim.ClaimType), q) {
		return true
	}
	if strings.Contains(strings.ToLower(claim.Reason), q) {
		return true
	}
	return strings.Contains(strings.ToLower(claim.Scope.Path), q)
}

// ClaimMatchesMemoryScope reports whether a claim is directly scoped to the
// task or session query.
func ClaimMatchesMemoryScope(claim MemoryClaim, scope MemoryQueryScope) bool {
	taskID := strings.TrimSpace(scope.TaskID)
	if taskID != "" && strings.TrimSpace(claim.Scope.TaskID) == taskID {
		return true
	}
	sessionID := strings.TrimSpace(scope.SessionID)
	return sessionID != "" && strings.TrimSpace(claim.Scope.SessionID) == sessionID
}

// ClaimVisibleForMemoryQuery applies query and scope visibility rules after
// lifecycle status filtering has already selected eligible statuses.
func ClaimVisibleForMemoryQuery(claim MemoryClaim, query string, scope MemoryQueryScope) bool {
	if ClaimMatchesMemoryScope(claim, scope) {
		return true
	}
	if !scope.HasScope() {
		return ClaimMatchesMemoryQuery(claim, query)
	}
	if strings.TrimSpace(query) == "" || claim.Status != ClaimStatusCurrent {
		return false
	}
	return ClaimMatchesMemoryQuery(claim, query)
}

// LimitMemoryQueryClaims caps claim results while preserving order.
func LimitMemoryQueryClaims(claims []MemoryClaim, limit int) []MemoryClaim {
	if limit > 0 && len(claims) > limit {
		return claims[:limit]
	}
	return claims
}
