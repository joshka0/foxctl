@tool
extends EditorPlugin
## GodotAIBridge - HTTP server for agentctl editor/godot skill.
##
## This plugin exposes the Godot Editor's scene manipulation APIs over HTTP,
## allowing AI agents to inspect and modify scenes programmatically.
##
## Installation:
## 1. Copy this folder to res://addons/godot_ai_bridge/
## 2. Enable the plugin in Project > Project Settings > Plugins
##
## The server listens on localhost:7777 by default.

# -- CONFIGURATION --
const PORT: int = 7777
const MAX_REQUEST_SIZE: int = 65536  # 64KB max request

# -- STATE --
var _server: TCPServer
var _clients: Array[StreamPeerTCP] = []
var _undo_redo: EditorUndoRedoManager

# -- HANDLERS --
var _core_handler: CoreHandler
var _resources_handler: ResourcesHandler
var _scripts_handler: ScriptsHandler
var _groups_handler: GroupsHandler
var _console_handler: ConsoleHandler
var _settings_handler: SettingsHandler
var _build_handler: BuildHandler
var _animation_handler: AnimationHandler
var _audio_handler: AudioHandler
var _input_handler: InputHandler
var _autoload_handler: AutoloadHandler
var _plugins_handler: PluginsHandler
var _theme_handler: ThemeHandler
var _shader_handler: ShaderHandler
var _tilemap_handler: TileMapHandler
var _physics_handler: PhysicsHandler
var _debug_handler: DebugHandler


func _enter_tree() -> void:
	_server = TCPServer.new()
	var err := _server.listen(PORT, "127.0.0.1")
	if err != OK:
		push_error("[GodotAIBridge] Failed to start server on port %d. Error: %d" % [PORT, err])
		return

	print("[GodotAIBridge] Listening on 127.0.0.1:%d" % PORT)
	_undo_redo = get_undo_redo()

	# Initialize handlers
	_core_handler = CoreHandler.new(_undo_redo)
	_resources_handler = ResourcesHandler.new(_undo_redo)
	_scripts_handler = ScriptsHandler.new()
	_groups_handler = GroupsHandler.new()
	_console_handler = ConsoleHandler.new()
	_settings_handler = SettingsHandler.new()
	_build_handler = BuildHandler.new()
	_animation_handler = AnimationHandler.new()
	_audio_handler = AudioHandler.new()
	_input_handler = InputHandler.new()
	_autoload_handler = AutoloadHandler.new()
	_plugins_handler = PluginsHandler.new()
	_theme_handler = ThemeHandler.new()
	_shader_handler = ShaderHandler.new()
	_tilemap_handler = TileMapHandler.new()
	_physics_handler = PhysicsHandler.new()
	_debug_handler = DebugHandler.new()


func _exit_tree() -> void:
	if _server:
		_server.stop()
	for client in _clients:
		client.disconnect_from_host()
	_clients.clear()

	# Cleanup handlers
	_core_handler = null
	_resources_handler = null
	_scripts_handler = null
	_groups_handler = null
	_console_handler = null
	_settings_handler = null
	_build_handler = null
	_animation_handler = null
	_audio_handler = null
	_input_handler = null
	_autoload_handler = null
	_plugins_handler = null
	_theme_handler = null
	_shader_handler = null
	_tilemap_handler = null
	_physics_handler = null
	_debug_handler = null

	print("[GodotAIBridge] Server stopped.")


func _process(_delta: float) -> void:
	if not _server:
		return

	# Accept new connections
	while _server.is_connection_available():
		var peer := _server.take_connection()
		if peer:
			_clients.append(peer)
			print("[GodotAIBridge] Client connected.")

	# Process existing clients
	var to_remove: Array[StreamPeerTCP] = []
	for client in _clients:
		var status := client.get_status()
		if status == StreamPeerTCP.STATUS_NONE or status == StreamPeerTCP.STATUS_ERROR:
			to_remove.append(client)
			continue

		if status == StreamPeerTCP.STATUS_CONNECTED and client.get_available_bytes() > 0:
			var request_str := _read_http_request(client)
			if request_str.is_empty():
				continue

			var response := _handle_request(request_str)
			_send_http_response(client, response)
			to_remove.append(client)  # Close after response (HTTP/1.0 style)

	for client in to_remove:
		client.disconnect_from_host()
		_clients.erase(client)


# -- HTTP HANDLING --

func _read_http_request(client: StreamPeerTCP) -> String:
	## Read an HTTP POST request and extract the JSON body.
	var available := client.get_available_bytes()
	if available <= 0 or available > MAX_REQUEST_SIZE:
		return ""

	var raw := client.get_data(available)
	if raw[0] != OK:
		return ""

	var request_bytes: PackedByteArray = raw[1]
	var request_text := request_bytes.get_string_from_utf8()

	# Find the body (after \r\n\r\n)
	var body_start := request_text.find("\r\n\r\n")
	if body_start == -1:
		return ""

	return request_text.substr(body_start + 4)


func _send_http_response(client: StreamPeerTCP, json_body: String) -> void:
	## Send an HTTP 200 response with JSON body.
	var body_bytes := json_body.to_utf8_buffer()
	var headers := "HTTP/1.1 200 OK\r\n"
	headers += "Content-Type: application/json\r\n"
	headers += "Content-Length: %d\r\n" % body_bytes.size()
	headers += "Connection: close\r\n"
	headers += "\r\n"

	client.put_data(headers.to_utf8_buffer())
	client.put_data(body_bytes)


# -- REQUEST ROUTING --

func _handle_request(json_str: String) -> String:
	## Parse JSON request and route to appropriate handler.
	var json := JSON.new()
	var err := json.parse(json_str)
	if err != OK:
		return _error_response("EJSON", "Invalid JSON: " + json.get_error_message(), "")

	var cmd: Dictionary = json.data
	if not cmd.has("action"):
		return _error_response("EARG", "Missing 'action' field", "")

	# Workspace validation
	if cmd.has("workspace_root"):
		var validation := _validate_workspace(cmd.workspace_root)
		if not validation.is_empty():
			return validation

	var action: String = cmd.action
	var params: Dictionary = cmd.get("params", {})

	# Route to appropriate handler
	var result := _route_action(action, params)
	return JSON.stringify(result)


func _route_action(action: String, params: Dictionary) -> Dictionary:
	## Route action to appropriate handler.

	# Core actions (ping, scene_tree, node_*, signal_connect, run_game, errors)
	if action in ["ping", "scene_tree", "node_inspect", "node_create", "node_delete",
				"node_rename", "node_reparent", "node_set_prop", "node_attach_script",
				"signal_connect", "class_info", "ensure_node", "search_nodes",
				"focus_node", "selection_state", "run_game", "run_scene", "stop_game", "errors"]:
		if not _core_handler:
			return _handler_not_initialized("core")
		return _core_handler.handle(action, params)

	# Resource actions (scene_save, scene_list, scene_open, scene_instance, resource_*, camera_*)
	if action in ["scene_save", "scene_list", "scene_open", "scene_instance",
				"resource_list", "search_resources", "resource_references",
				"camera_save", "camera_restore", "camera_list"]:
		if not _resources_handler:
			return _handler_not_initialized("resources")
		return _resources_handler.handle(action, params)

	# Script actions (script_create, script_read, script_edit)
	if action in ["script_create", "script_read", "script_edit"]:
		if not _scripts_handler:
			return _handler_not_initialized("scripts")
		return _scripts_handler.handle(action, params)

	# Group actions
	if action in ["group_add", "group_remove", "group_list"]:
		if not _groups_handler:
			return _handler_not_initialized("groups")
		return _groups_handler.handle(action, params)

	# Console actions
	if action == "console_output":
		if not _console_handler:
			return _handler_not_initialized("console")
		return _console_handler.handle(action, params)

	# Settings actions
	if action in ["project_setting_get", "project_setting_set"]:
		if not _settings_handler:
			return _handler_not_initialized("settings")
		return _settings_handler.handle(action, params)

	# Build actions
	if action == "build":
		if not _build_handler:
			return _handler_not_initialized("build")
		return _build_handler.handle(action, params)

	# Animation actions
	if action in ["animation_list", "animation_play", "animation_stop"]:
		if not _animation_handler:
			return _handler_not_initialized("animation")
		return _animation_handler.handle(action, params)

	# Audio actions
	if action in ["audio_play", "audio_stop"]:
		if not _audio_handler:
			return _handler_not_initialized("audio")
		return _audio_handler.handle(action, params)

	# Input actions
	if action in ["input_action_list", "input_action_add", "input_action_remove"]:
		if not _input_handler:
			return _handler_not_initialized("input")
		return _input_handler.handle(action, params)

	# Autoload actions
	if action in ["autoload_list", "autoload_add", "autoload_remove"]:
		if not _autoload_handler:
			return _handler_not_initialized("autoload")
		return _autoload_handler.handle(action, params)

	# Plugin actions
	if action in ["plugin_list", "plugin_enable", "plugin_disable"]:
		if not _plugins_handler:
			return _handler_not_initialized("plugins")
		return _plugins_handler.handle(action, params)

	# Theme actions
	if action in ["theme_get", "theme_set"]:
		if not _theme_handler:
			return _handler_not_initialized("theme")
		return _theme_handler.handle(action, params)

	# Shader actions
	if action in ["shader_create", "shader_edit"]:
		if not _shader_handler:
			return _handler_not_initialized("shader")
		return _shader_handler.handle(action, params)

	# TileMap actions
	if action in ["tilemap_get_cell", "tilemap_set_cell"]:
		if not _tilemap_handler:
			return _handler_not_initialized("tilemap")
		return _tilemap_handler.handle(action, params)

	# Physics actions
	if action in ["physics_layer_get", "physics_layer_set"]:
		if not _physics_handler:
			return _handler_not_initialized("physics")
		return _physics_handler.handle(action, params)

	# Debug actions
	if action in ["debug_draw_enable", "debug_draw_disable"]:
		if not _debug_handler:
			return _handler_not_initialized("debug")
		return _debug_handler.handle(action, params)

	# Unknown action
	return {
		"status": "error",
		"data": null,
		"error": {
			"code": "EARG",
			"message": "Unknown action: " + action,
			"hint": "Valid actions: ping, scene_tree, node_inspect, node_create, node_delete, node_rename, node_reparent, node_set_prop, node_attach_script, signal_connect, class_info, ensure_node, scene_save, scene_list, scene_open, scene_instance, search_nodes, focus_node, selection_state, camera_save, camera_restore, camera_list, script_create, script_read, script_edit, group_add, group_remove, group_list, console_output, project_setting_get, project_setting_set, build, animation_list, animation_play, animation_stop, audio_play, audio_stop, input_action_list, input_action_add, input_action_remove, autoload_list, autoload_add, autoload_remove, plugin_list, plugin_enable, plugin_disable, theme_get, theme_set, shader_create, shader_edit, tilemap_get_cell, tilemap_set_cell, physics_layer_get, physics_layer_set, debug_draw_enable, debug_draw_disable, resource_list, search_resources, resource_references, run_game, run_scene, stop_game, errors",
		},
	}


func _validate_workspace(workspace_root: String) -> String:
	## Validate that the workspace matches the Godot project root.
	## Returns empty string if valid, error response if not.
	var project_root := ProjectSettings.globalize_path("res://")
	# Normalize paths (remove trailing slashes)
	workspace_root = workspace_root.rstrip("/\\")
	project_root = project_root.rstrip("/\\")

	# Check if they match or one is a subdirectory of the other
	if workspace_root == project_root:
		return ""
	if workspace_root.begins_with(project_root) or project_root.begins_with(workspace_root):
		return ""

	return _error_response("EWORKSPACE_MISMATCH",
		"Workspace mismatch: agent workspace '%s' != Godot project '%s'" % [workspace_root, project_root],
		"Ensure agentctl is running from the Godot project directory, or use --workspace to specify the correct path.")


# -- RESPONSE HELPERS --

func _handler_not_initialized(handler_name: String) -> Dictionary:
	return {
		"status": "error",
		"data": null,
		"error": {
			"code": "ERUNTIME",
			"message": "Handler not initialized: " + handler_name,
			"hint": "The plugin may not have been properly initialized. Try disabling and re-enabling the plugin.",
		},
	}


func _error_response(code: String, message: String, hint: String) -> String:
	# Add to error buffer via core handler
	if _core_handler:
		_core_handler.add_error(message, code, "error")
	return JSON.stringify({
		"status": "error",
		"data": null,
		"error": {
			"code": code,
			"message": message,
			"hint": hint,
		},
	})
