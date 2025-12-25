package handlers

import (
	"fmt"
	"strings"
)

// TileMapHandler handles tilemap actions: tilemap_get_cell, tilemap_set_cell.
type TileMapHandler struct{}

func init() {
	h := &TileMapHandler{}
	Register(ActionTileMapGetCell, h)
	Register(ActionTileMapSetCell, h)
}

func (h *TileMapHandler) Validate(in Input) error {
	switch in.Action {
	case ActionTileMapGetCell, ActionTileMapSetCell:
		if strings.TrimSpace(in.NodePath) == "" {
			return fmt.Errorf("node_path is required for action %q", in.Action)
		}
		// x and y default to 0, which is valid
	}
	return nil
}

func (h *TileMapHandler) BuildParams(in Input) map[string]any {
	params := make(map[string]any)

	params["node_path"] = in.NodePath
	params["x"] = in.TileX
	params["y"] = in.TileY
	params["layer"] = in.Layer

	if in.Action == ActionTileMapSetCell {
		if in.Erase {
			params["erase"] = true
		} else {
			params["source_id"] = in.SourceID
			params["atlas_x"] = in.AtlasX
			params["atlas_y"] = in.AtlasY
			params["alternative_tile"] = in.AlternativeTile
		}
	}

	return params
}

func (h *TileMapHandler) GenerateSummary(action string, data any) string {
	m, _ := data.(map[string]any)

	switch action {
	case ActionTileMapGetCell:
		if m != nil {
			x, _ := m["x"].(float64)
			y, _ := m["y"].(float64)
			isEmpty, _ := m["is_empty"].(bool)
			if isEmpty {
				return fmt.Sprintf("Cell at (%d, %d) is empty", int(x), int(y))
			}
			return fmt.Sprintf("Retrieved cell at (%d, %d)", int(x), int(y))
		}
		return "Retrieved tile cell"

	case ActionTileMapSetCell:
		if m != nil {
			x, _ := m["x"].(float64)
			y, _ := m["y"].(float64)
			erased, _ := m["erased"].(bool)
			if erased {
				return fmt.Sprintf("Erased cell at (%d, %d)", int(x), int(y))
			}
			return fmt.Sprintf("Set cell at (%d, %d)", int(x), int(y))
		}
		return "Set tile cell"

	default:
		return fmt.Sprintf("Completed action: %s", action)
	}
}
