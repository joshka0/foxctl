package room

import "sort"

// sortRoomsByCreated sorts in place, oldest first. Pulled out so the main
// file can stay focused on the public surface.
func sortRoomsByCreated(rs []Room) {
	sort.Slice(rs, func(i, j int) bool {
		if rs[i].CreatedAt.Equal(rs[j].CreatedAt) {
			return rs[i].ID < rs[j].ID
		}
		return rs[i].CreatedAt.Before(rs[j].CreatedAt)
	})
}
