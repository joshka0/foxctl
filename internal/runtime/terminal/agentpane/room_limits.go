package agentpane

// EffectiveRoomLimit returns the configured room limit when set, otherwise the
// adapter default.
func EffectiveRoomLimit(configured, fallback int) int {
	if configured > 0 {
		return configured
	}
	return fallback
}

// RoomLimitReached reports whether the current occupancy has reached the
// effective room limit. A non-positive limit means unlimited.
func RoomLimitReached(current, limit int) bool {
	return limit > 0 && current >= limit
}
