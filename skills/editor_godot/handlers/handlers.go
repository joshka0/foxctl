// Package handlers provides modular action handlers for the editor/godot skill.
package handlers

import "sort"

// Action constants organized by category.
const (
	// Core actions
	ActionPing             = "ping"
	ActionSceneTree        = "scene_tree"
	ActionNodeInspect      = "node_inspect"
	ActionNodeCreate       = "node_create"
	ActionNodeDelete       = "node_delete"
	ActionNodeRename       = "node_rename"
	ActionNodeReparent     = "node_reparent"
	ActionNodeSetProp      = "node_set_prop"
	ActionNodeAttachScript = "node_attach_script"
	ActionSignalConnect    = "signal_connect"
	ActionClassInfo        = "class_info"
	ActionEnsureNode       = "ensure_node"
	ActionSearchNodes      = "search_nodes"
	ActionFocusNode        = "focus_node"
	ActionSelectionState   = "selection_state"
	ActionRunGame          = "run_game"
	ActionRunScene         = "run_scene"
	ActionStopGame         = "stop_game"
	ActionErrors           = "errors"

	// Resource actions
	ActionSceneSave          = "scene_save"
	ActionSceneList          = "scene_list"
	ActionSceneOpen          = "scene_open"
	ActionSceneInstance      = "scene_instance"
	ActionResourceList       = "resource_list"
	ActionSearchResources    = "search_resources"
	ActionResourceReferences = "resource_references"
	ActionCameraSave         = "camera_save"
	ActionCameraRestore      = "camera_restore"
	ActionCameraList         = "camera_list"

	// Script actions
	ActionScriptCreate = "script_create"
	ActionScriptRead   = "script_read"
	ActionScriptEdit   = "script_edit"

	// Group actions
	ActionGroupAdd    = "group_add"
	ActionGroupRemove = "group_remove"
	ActionGroupList   = "group_list"

	// Console actions
	ActionConsoleOutput = "console_output"

	// Settings actions
	ActionProjectSettingGet = "project_setting_get"
	ActionProjectSettingSet = "project_setting_set"

	// Build actions
	ActionBuild = "build"

	// Animation actions
	ActionAnimationList = "animation_list"
	ActionAnimationPlay = "animation_play"
	ActionAnimationStop = "animation_stop"

	// Audio actions
	ActionAudioPlay = "audio_play"
	ActionAudioStop = "audio_stop"

	// Input actions
	ActionInputActionList   = "input_action_list"
	ActionInputActionAdd    = "input_action_add"
	ActionInputActionRemove = "input_action_remove"

	// Autoload actions
	ActionAutoloadList   = "autoload_list"
	ActionAutoloadAdd    = "autoload_add"
	ActionAutoloadRemove = "autoload_remove"

	// Plugin actions
	ActionPluginList    = "plugin_list"
	ActionPluginEnable  = "plugin_enable"
	ActionPluginDisable = "plugin_disable"

	// Theme actions
	ActionThemeGet = "theme_get"
	ActionThemeSet = "theme_set"

	// Shader actions
	ActionShaderCreate = "shader_create"
	ActionShaderEdit   = "shader_edit"

	// TileMap actions
	ActionTileMapGetCell = "tilemap_get_cell"
	ActionTileMapSetCell = "tilemap_set_cell"

	// Physics actions
	ActionPhysicsLayerGet = "physics_layer_get"
	ActionPhysicsLayerSet = "physics_layer_set"

	// Debug actions
	ActionDebugDrawEnable  = "debug_draw_enable"
	ActionDebugDrawDisable = "debug_draw_disable"
)

// Input represents the skill input parameters.
type Input struct {
	Action        string            `json:"action"`
	Host          string            `json:"host"`
	Port          int               `json:"port"`
	TimeoutMS     int               `json:"timeout_ms"`
	NodePath      string            `json:"node_path"`
	ParentPath    string            `json:"parent_path"`
	NewParentPath string            `json:"new_parent_path"`
	NodeType      string            `json:"node_type"`
	NodeName      string            `json:"node_name"`
	NewName       string            `json:"new_name"`
	Property      string            `json:"property"`
	Value         string            `json:"value"`
	ScriptPath    string            `json:"script_path"`
	SignalName    string            `json:"signal_name"`
	TargetPath    string            `json:"target_path"`
	MethodName    string            `json:"method_name"`
	ClassName     string            `json:"class_name"`
	ResourcePath  string            `json:"resource_path"`
	Pattern       string            `json:"pattern"`
	MaxDepth      int               `json:"max_depth"`
	MaxNodes      int               `json:"max_nodes"`
	MaxResults    int               `json:"max_results"`
	ErrorLimit    int               `json:"error_limit"`
	Props         map[string]string `json:"props"`
	IfExists      string            `json:"if_exists"`
	ScenePath     string            `json:"scene_path"`
	Recursive     bool              `json:"recursive"`
	InstanceName  string            `json:"instance_name"`
	SearchName    string            `json:"search_name"`
	SearchType    string            `json:"search_type"`
	Frame         bool              `json:"frame"`
	ExtendsClass  string            `json:"extends"`
	Exports       []any             `json:"exports"`
	Methods       []any             `json:"methods"`
	Signals       []string          `json:"signals"`
	Overwrite     bool              `json:"overwrite"`
	ResourceType  string            `json:"resource_type"`
	BookmarkName  string            `json:"bookmark_name"`
	DryRun        bool              `json:"dry_run"`

	// New fields for additional actions
	GroupName       string  `json:"group_name"`
	SettingName     string  `json:"setting_name"`
	SettingValue    any     `json:"setting_value"`
	PresetName      string  `json:"preset_name"`
	OutputPath      string  `json:"output_path"`
	AnimationName   string  `json:"animation_name"`
	AutoloadName    string  `json:"autoload_name"`
	PluginName      string  `json:"plugin_name"`
	ShaderType      string  `json:"shader_type"`
	ShaderCode      string  `json:"shader_code"`
	ThemeType       string  `json:"theme_type"`
	ThemeName       string  `json:"theme_name"`
	ThemeValue      any     `json:"theme_value"`
	Layer           int     `json:"layer"`
	Dimension       string  `json:"dimension"`
	DebugMode       string  `json:"debug_mode"`
	TileX           int     `json:"x"`
	TileY           int     `json:"y"`
	SourceID        int     `json:"source_id"`
	AtlasX          int     `json:"atlas_x"`
	AtlasY          int     `json:"atlas_y"`
	AlternativeTile int     `json:"alternative_tile"`
	Erase           bool    `json:"erase"`
	Content         string  `json:"content"`
	StartLine       int     `json:"start_line"`
	EndLine         int     `json:"end_line"`
	InputEvent      any     `json:"input_event"`
	Enabled         bool    `json:"enabled"`
	Loop            bool    `json:"loop"`
	FromPosition    float64 `json:"from_position"`
}

// Handler defines the interface for action handlers.
type Handler interface {
	// Validate checks if the input is valid for the action.
	Validate(in Input) error
	// BuildParams constructs the parameters for the plugin request.
	BuildParams(in Input) map[string]any
	// GenerateSummary creates a human-readable summary of the result.
	GenerateSummary(action string, data any) string
}

// Registry maps actions to their handlers.
var Registry = make(map[string]Handler)

// Register adds a handler to the registry.
func Register(action string, handler Handler) {
	Registry[action] = handler
}

// GetHandler returns the handler for an action.
func GetHandler(action string) (Handler, bool) {
	h, ok := Registry[action]
	return h, ok
}

// AllActions returns a sorted list of all registered actions.
func AllActions() []string {
	actions := make([]string, 0, len(Registry))
	for action := range Registry {
		actions = append(actions, action)
	}
	sort.Strings(actions)
	return actions
}
