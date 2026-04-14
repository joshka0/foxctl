package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/oputil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
)

const command = "unity/input"

var allowedOperations = []string{
	"list_maps",
	"list_actions",
	"add_map",
	"add_action",
	"add_binding",
	"remove_map",
	"remove_action",
}

// Input defines the expected skill payload for unity/input operations.
type Input struct {
	Operation   string `json:"operation"`
	InputFile   string `json:"input_file"`
	MapName     string `json:"map_name"`
	ActionName  string `json:"action_name"`
	ActionType  string `json:"action_type"`
	BindingPath string `json:"binding_path"`
	ProjectPath string `json:"project_path"`
}

// InputActions represents the top-level .inputactions file structure.
type InputActions struct {
	Name           string            `json:"name"`
	Maps           []ActionMap       `json:"maps"`
	ControlSchemes []json.RawMessage `json:"controlSchemes"`
}

// ActionMap represents an input action map (e.g., "Player", "UI").
type ActionMap struct {
	Name     string    `json:"name"`
	ID       string    `json:"id"`
	Actions  []Action  `json:"actions"`
	Bindings []Binding `json:"bindings"`
}

// Action represents a single input action within a map.
type Action struct {
	Name                string `json:"name"`
	Type                string `json:"type"`
	ID                  string `json:"id"`
	ExpectedControlType string `json:"expectedControlType"`
	Processors          string `json:"processors"`
	Interactions        string `json:"interactions"`
}

// Binding represents an input binding for an action.
type Binding struct {
	Name              string `json:"name"`
	ID                string `json:"id"`
	Path              string `json:"path"`
	Interactions      string `json:"interactions"`
	Processors        string `json:"processors"`
	Groups            string `json:"groups"`
	Action            string `json:"action"`
	IsComposite       bool   `json:"isComposite"`
	IsPartOfComposite bool   `json:"isPartOfComposite"`
}

func main() {
	skillmain.Main(command, run)
}

func run(_ context.Context, rc *skillmain.RunContext, in Input) error {
	op := oputil.Op(in.Operation)
	opHint := fmt.Sprintf("Use one of: %s.", strings.Join(allowedOperations, ", "))
	if op == "" {
		return skillerr.Arg("operation is required", skillerr.WithHint(opHint))
	}
	if err := oputil.Validate(op, allowedOperations...); err != nil {
		return skillerr.Arg(err.Error(), skillerr.WithHint(opHint))
	}

	projectPath, err := resolveProjectPath(rc, in.ProjectPath)
	if err != nil {
		return err
	}
	if err := validateProjectPath(projectPath); err != nil {
		return err
	}

	inputFilePath, err := resolveInputFilePath(projectPath, in.InputFile)
	if err != nil {
		return err
	}

	result, err := oputil.NewSwitch(op).
		Case("list_maps", func() (map[string]any, error) {
			return listMaps(inputFilePath)
		}).
		Case("list_actions", func() (map[string]any, error) {
			return listActions(inputFilePath, strings.TrimSpace(in.MapName))
		}).
		Case("add_map", func() (map[string]any, error) {
			return addMap(inputFilePath, strings.TrimSpace(in.MapName))
		}).
		Case("add_action", func() (map[string]any, error) {
			return addAction(inputFilePath, strings.TrimSpace(in.MapName), strings.TrimSpace(in.ActionName), strings.TrimSpace(in.ActionType))
		}).
		Case("add_binding", func() (map[string]any, error) {
			return addBinding(inputFilePath, strings.TrimSpace(in.MapName), strings.TrimSpace(in.ActionName), strings.TrimSpace(in.BindingPath))
		}).
		Case("remove_map", func() (map[string]any, error) {
			return removeMap(inputFilePath, strings.TrimSpace(in.MapName))
		}).
		Case("remove_action", func() (map[string]any, error) {
			return removeAction(inputFilePath, strings.TrimSpace(in.MapName), strings.TrimSpace(in.ActionName))
		}).
		Run()
	if err != nil {
		return err
	}

	return skillout.Emit(rc, command, result)
}

func resolveProjectPath(rc *skillmain.RunContext, projectPath string) (string, error) {
	resolved := strings.TrimSpace(projectPath)
	if resolved == "" {
		resolved = rc.PathValidator.Workspace()
	}
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(rc.PathValidator.Workspace(), resolved)
	}
	return filepath.Clean(resolved), nil
}

func validateProjectPath(projectPath string) error {
	info, err := os.Stat(projectPath)
	if err != nil {
		if os.IsNotExist(err) {
			return skillerr.NotFound(
				fmt.Sprintf("project path not found: %s", projectPath),
				skillerr.WithHint("Provide an existing Unity project path with an Assets/ directory."),
			)
		}
		return skillerr.WrapIO("validate project path", err)
	}
	if !info.IsDir() {
		return skillerr.Arg(
			fmt.Sprintf("project path is not a directory: %s", projectPath),
			skillerr.WithHint("Provide a Unity project directory containing an Assets/ directory."),
		)
	}

	assetsPath := filepath.Join(projectPath, "Assets")
	if _, err := os.Stat(assetsPath); err != nil {
		if os.IsNotExist(err) {
			return skillerr.NotFound(
				fmt.Sprintf("Assets directory not found in project path: %s", projectPath),
				skillerr.WithHint("Unity project directories must include an Assets/ directory."),
			)
		}
		return skillerr.WrapIO("validate Assets directory", err)
	}

	return nil
}

func resolveInputFilePath(projectPath, inputFile string) (string, error) {
	cleanInput := strings.TrimSpace(inputFile)
	if cleanInput == "" {
		return "", skillerr.Arg(
			"input_file is required",
			skillerr.WithHint("Provide the path to a .inputactions file (relative to project_path or workspace)."),
		)
	}

	if !filepath.IsAbs(cleanInput) {
		cleanInput = filepath.Join(projectPath, cleanInput)
	}
	return filepath.Clean(cleanInput), nil
}

func readInputActions(path string) (InputActions, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return InputActions{}, skillerr.NotFound(
				fmt.Sprintf("input file not found: %s", path),
				skillerr.WithHint("Provide a valid .inputactions file path."),
			)
		}
		return InputActions{}, skillerr.WrapIO("read input file", err)
	}

	var parsed InputActions
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return InputActions{}, skillerr.WrapParse("parse .inputactions JSON", err)
	}

	return parsed, nil
}

func writeInputActions(path string, data InputActions) error {
	marshaled, err := json.MarshalIndent(data, "", "    ")
	if err != nil {
		return skillerr.WrapParse("marshal .inputactions JSON", err)
	}
	marshaled = append(marshaled, '\n')
	if err := os.WriteFile(path, marshaled, 0o644); err != nil {
		return skillerr.WrapIO("write .inputactions file", err)
	}
	return nil
}

func findMapIndex(maps []ActionMap, name string) int {
	for i, m := range maps {
		if m.Name == name {
			return i
		}
	}
	return -1
}

func findActionIndex(actions []Action, name string) int {
	for i, a := range actions {
		if a.Name == name {
			return i
		}
	}
	return -1
}

func listMaps(inputFile string) (map[string]any, error) {
	data, err := readInputActions(inputFile)
	if err != nil {
		return nil, err
	}

	maps := make([]map[string]any, 0, len(data.Maps))
	for _, m := range data.Maps {
		maps = append(maps, map[string]any{
			"name":          m.Name,
			"actions":       len(m.Actions),
			"bindings":      len(m.Bindings),
			"action_count":  len(m.Actions),
			"binding_count": len(m.Bindings),
		})
	}

	return map[string]any{
		"operation": "list_maps",
		"maps":      maps,
		"count":     len(maps),
	}, nil
}

func listActions(inputFile, mapName string) (map[string]any, error) {
	if mapName == "" {
		return nil, skillerr.Arg("map_name is required for list_actions", skillerr.WithHint("Provide the name of an action map to inspect."))
	}

	data, err := readInputActions(inputFile)
	if err != nil {
		return nil, err
	}

	mapIdx := findMapIndex(data.Maps, mapName)
	if mapIdx < 0 {
		return nil, skillerr.NotFound(
			fmt.Sprintf("map not found: %s", mapName),
			skillerr.WithHint("Create the map first, then list its actions."),
		)
	}
	targetMap := data.Maps[mapIdx]
	actions := make([]map[string]any, 0, len(targetMap.Actions))

	for _, action := range targetMap.Actions {
		bindingCount := 0
		actionRef := fmt.Sprintf("%s/%s", targetMap.Name, action.Name)
		for _, binding := range targetMap.Bindings {
			if binding.Action == actionRef {
				bindingCount++
			}
		}
		actions = append(actions, map[string]any{
			"name":          action.Name,
			"type":          action.Type,
			"binding_count": bindingCount,
		})
	}

	return map[string]any{
		"operation":    "list_actions",
		"map_name":     targetMap.Name,
		"actions":      actions,
		"action_count": len(actions),
	}, nil
}

func addMap(inputFile, mapName string) (map[string]any, error) {
	if mapName == "" {
		return nil, skillerr.Arg("map_name is required for add_map", skillerr.WithHint("Provide a unique name for the new action map."))
	}

	data, err := readInputActions(inputFile)
	if err != nil {
		return nil, err
	}
	if findMapIndex(data.Maps, mapName) >= 0 {
		return nil, skillerr.Arg(
			fmt.Sprintf("map already exists: %s", mapName),
			skillerr.WithHint("Use a unique map_name."),
		)
	}

	data.Maps = append(data.Maps, ActionMap{
		Name:     mapName,
		ID:       generateUUID(),
		Actions:  []Action{},
		Bindings: []Binding{},
	})

	if err := writeInputActions(inputFile, data); err != nil {
		return nil, err
	}

	return map[string]any{
		"operation": "add_map",
		"map_name":  mapName,
		"message":   "map added",
	}, nil
}

func addAction(inputFile, mapName, actionName, actionType string) (map[string]any, error) {
	if mapName == "" {
		return nil, skillerr.Arg("map_name is required for add_action", skillerr.WithHint("Specify which action map to add the action to."))
	}
	if actionName == "" {
		return nil, skillerr.Arg("action_name is required for add_action", skillerr.WithHint("Provide a name for the new action."))
	}
	if actionType == "" {
		return nil, skillerr.Arg("action_type is required for add_action", skillerr.WithHint("Use one of: Button, Value, PassThrough."))
	}

	normalizedType, expectedControlType, err := resolveActionType(actionType)
	if err != nil {
		return nil, err
	}

	data, err := readInputActions(inputFile)
	if err != nil {
		return nil, err
	}

	mapIdx := findMapIndex(data.Maps, mapName)
	if mapIdx < 0 {
		return nil, skillerr.NotFound(
			fmt.Sprintf("map not found: %s", mapName),
			skillerr.WithHint("Create the map first, then add actions."),
		)
	}
	targetMap := &data.Maps[mapIdx]
	if findActionIndex(targetMap.Actions, actionName) >= 0 {
		return nil, skillerr.Arg(
			fmt.Sprintf("action already exists: %s", actionName),
			skillerr.WithHint("Use a unique action_name in this map."),
		)
	}

	targetMap.Actions = append(targetMap.Actions, Action{
		Name:                actionName,
		Type:                normalizedType,
		ID:                  generateUUID(),
		ExpectedControlType: expectedControlType,
		Processors:          "",
		Interactions:        "",
	})

	if err := writeInputActions(inputFile, data); err != nil {
		return nil, err
	}

	return map[string]any{
		"operation":   "add_action",
		"map_name":    mapName,
		"action_name": actionName,
		"action_type": normalizedType,
		"message":     "action added",
	}, nil
}

func addBinding(inputFile, mapName, actionName, bindingPath string) (map[string]any, error) {
	if mapName == "" {
		return nil, skillerr.Arg("map_name is required for add_binding", skillerr.WithHint("Specify which action map contains the target action."))
	}
	if actionName == "" {
		return nil, skillerr.Arg("action_name is required for add_binding", skillerr.WithHint("Specify which action to bind to."))
	}
	if bindingPath == "" {
		return nil, skillerr.Arg("binding_path is required for add_binding", skillerr.WithHint("Example: <Keyboard>/w, <Gamepad>/leftStick."))
	}

	data, err := readInputActions(inputFile)
	if err != nil {
		return nil, err
	}

	mapIdx := findMapIndex(data.Maps, mapName)
	if mapIdx < 0 {
		return nil, skillerr.NotFound(
			fmt.Sprintf("map not found: %s", mapName),
			skillerr.WithHint("Create the map first, then add bindings."),
		)
	}
	targetMap := &data.Maps[mapIdx]
	if findActionIndex(targetMap.Actions, actionName) < 0 {
		return nil, skillerr.NotFound(
			fmt.Sprintf("action not found: %s", actionName),
			skillerr.WithHint("Create the action first, then add bindings."),
		)
	}

	targetMap.Bindings = append(targetMap.Bindings, Binding{
		Name:              "",
		ID:                generateUUID(),
		Path:              bindingPath,
		Interactions:      "",
		Processors:        "",
		Groups:            "",
		Action:            fmt.Sprintf("%s/%s", mapName, actionName),
		IsComposite:       false,
		IsPartOfComposite: false,
	})

	if err := writeInputActions(inputFile, data); err != nil {
		return nil, err
	}

	return map[string]any{
		"operation":     "add_binding",
		"map_name":      mapName,
		"action_name":   actionName,
		"binding_path":  bindingPath,
		"binding_count": len(targetMap.Bindings),
		"message":       "binding added",
	}, nil
}

func removeMap(inputFile, mapName string) (map[string]any, error) {
	if mapName == "" {
		return nil, skillerr.Arg("map_name is required for remove_map", skillerr.WithHint("Run list_maps to see existing maps."))
	}

	data, err := readInputActions(inputFile)
	if err != nil {
		return nil, err
	}

	mapIdx := findMapIndex(data.Maps, mapName)
	if mapIdx < 0 {
		return nil, skillerr.NotFound(
			fmt.Sprintf("map not found: %s", mapName),
			skillerr.WithHint("Use list_maps to see existing maps."),
		)
	}

	data.Maps = append(data.Maps[:mapIdx], data.Maps[mapIdx+1:]...)
	if err := writeInputActions(inputFile, data); err != nil {
		return nil, err
	}

	return map[string]any{
		"operation": "remove_map",
		"map_name":  mapName,
		"message":   "map removed",
	}, nil
}

func removeAction(inputFile, mapName, actionName string) (map[string]any, error) {
	if mapName == "" {
		return nil, skillerr.Arg("map_name is required for remove_action", skillerr.WithHint("Specify which action map contains the action."))
	}
	if actionName == "" {
		return nil, skillerr.Arg("action_name is required for remove_action", skillerr.WithHint("Run list_actions to see existing actions."))
	}

	data, err := readInputActions(inputFile)
	if err != nil {
		return nil, err
	}

	mapIdx := findMapIndex(data.Maps, mapName)
	if mapIdx < 0 {
		return nil, skillerr.NotFound(
			fmt.Sprintf("map not found: %s", mapName),
			skillerr.WithHint("Use list_maps to see existing maps."),
		)
	}
	targetMap := &data.Maps[mapIdx]

	actionIdx := findActionIndex(targetMap.Actions, actionName)
	if actionIdx < 0 {
		return nil, skillerr.NotFound(
			fmt.Sprintf("action not found: %s", actionName),
			skillerr.WithHint("Use list_actions to see existing actions."),
		)
	}
	targetMap.Actions = append(targetMap.Actions[:actionIdx], targetMap.Actions[actionIdx+1:]...)

	actionRef := fmt.Sprintf("%s/%s", mapName, actionName)
	filteredBindings := targetMap.Bindings[:0]
	for _, binding := range targetMap.Bindings {
		if binding.Action != actionRef {
			filteredBindings = append(filteredBindings, binding)
		}
	}
	targetMap.Bindings = filteredBindings

	if err := writeInputActions(inputFile, data); err != nil {
		return nil, err
	}

	return map[string]any{
		"operation":   "remove_action",
		"map_name":    mapName,
		"action_name": actionName,
		"message":     "action removed",
	}, nil
}

func resolveActionType(actionType string) (string, string, error) {
	switch strings.ToLower(actionType) {
	case "button":
		return "Button", "Button", nil
	case "value":
		return "Value", "Analog", nil
	case "passthrough":
		return "PassThrough", "", nil
	default:
		return "", "", skillerr.Arg(
			fmt.Sprintf("invalid action_type %q", actionType),
			skillerr.WithHint("Use one of: Button, Value, PassThrough."),
		)
	}
}

func generateUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
