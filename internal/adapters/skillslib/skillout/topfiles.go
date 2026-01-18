// Package skillout provides shared output helpers for skills.
package skillout

import "sort"

// SummarizeTopFiles returns the top files by count as [][2]any.
func SummarizeTopFiles(counts map[string]int, limit int) [][2]any {
	type kv struct {
		File  string
		Count int
	}
	list := make([]kv, 0, len(counts))
	for file, count := range counts {
		list = append(list, kv{File: file, Count: count})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Count == list[j].Count {
			return list[i].File < list[j].File
		}
		return list[i].Count > list[j].Count
	})
	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}
	out := make([][2]any, 0, len(list))
	for _, item := range list {
		out = append(out, [2]any{item.File, item.Count})
	}
	return out
}
