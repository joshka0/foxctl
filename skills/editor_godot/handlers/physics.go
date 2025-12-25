package handlers

import "fmt"

// PhysicsHandler handles physics actions: physics_layer_get, physics_layer_set.
type PhysicsHandler struct{}

func init() {
	h := &PhysicsHandler{}
	Register(ActionPhysicsLayerGet, h)
	Register(ActionPhysicsLayerSet, h)
}

func (h *PhysicsHandler) Validate(in Input) error {
	switch in.Action {
	case ActionPhysicsLayerGet:
		// layer is optional (-1 means all layers)
		// dimension defaults to "2d"
	case ActionPhysicsLayerSet:
		if in.Layer < 0 || in.Layer > 31 {
			return fmt.Errorf("layer must be between 0 and 31 for action %q", in.Action)
		}
	}
	return nil
}

func (h *PhysicsHandler) BuildParams(in Input) map[string]any {
	params := make(map[string]any)

	if in.Layer >= 0 {
		params["layer"] = in.Layer
	} else {
		params["layer"] = -1 // Get all layers
	}

	dimension := in.Dimension
	if dimension == "" {
		dimension = "2d"
	}
	params["dimension"] = dimension

	if in.Action == ActionPhysicsLayerSet {
		params["name"] = in.SearchName // Using SearchName for layer name
	}

	return params
}

func (h *PhysicsHandler) GenerateSummary(action string, data any) string {
	m, _ := data.(map[string]any)

	switch action {
	case ActionPhysicsLayerGet:
		if m != nil {
			dimension, _ := m["dimension"].(string)
			if layers, ok := m["layers"].([]any); ok {
				return fmt.Sprintf("Found %d configured %s physics layer(s)", len(layers), dimension)
			}
			layer, _ := m["layer"].(float64)
			name, _ := m["name"].(string)
			if name != "" {
				return fmt.Sprintf("Layer %d: '%s'", int(layer), name)
			}
			return fmt.Sprintf("Retrieved layer %d info", int(layer))
		}
		return "Retrieved physics layer info"

	case ActionPhysicsLayerSet:
		if m != nil {
			layer, _ := m["layer"].(float64)
			newName, _ := m["new_name"].(string)
			return fmt.Sprintf("Set layer %d name to '%s'", int(layer), newName)
		}
		return "Set physics layer"

	default:
		return fmt.Sprintf("Completed action: %s", action)
	}
}
