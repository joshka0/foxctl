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
var _error_buffer: Array[Dictionary] = []
const MAX_ERROR_BUFFER: int = 200


func _enter_tree() -> void:
	_server = TCPServer.new()
	var err := _server.listen(PORT, "127.0.0.1")
	if err != OK:
		push_error("[GodotAIBridge] Failed to start server on port %d. Error: %d" % [PORT, err])
		return
	
	print("[GodotAIBridge] Listening on 127.0.0.1:%d" % PORT)
	_undo_redo = get_undo_redo()
	
	# Connect to error output for capturing errors
	# Note: In Godot 4, we'd ideally hook into the output panel, but that's complex.
	# For now, errors are captured via push_error calls we can intercept.


func _exit_tree() -> void:
	if _server:
		_server.stop()
	for client in _clients:
		client.disconnect_from_host()
	_clients.clear()
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
	
	match action:
		"ping":
			return _handle_ping()
		"scene_tree":
			return _handle_scene_tree(params)
		"node_inspect":
			return _handle_node_inspect(params)
		"node_create":
			return _handle_node_create(params)
		"node_set_prop":
			return _handle_node_set_prop(params)
		"node_attach_script":
			return _handle_node_attach_script(params)
		"signal_connect":
			return _handle_signal_connect(params)
		"run_game":
			return _handle_run_game()
		"errors":
			return _handle_errors(params)
		_:
			return _error_response("EACTION", "Unknown action: " + action, 
				"Valid actions: ping, scene_tree, node_inspect, node_create, node_set_prop, node_attach_script, signal_connect, run_game, errors")


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


# -- ACTION HANDLERS --

func _handle_ping() -> String:
	var project_root := ProjectSettings.globalize_path("res://")
	var project_name := ProjectSettings.get_setting("application/config/name", "Unknown")
	return _success_response({
		"pong": true,
		"project_root": project_root,
		"project_name": project_name,
		"godot_version": Engine.get_version_info().string,
	})


func _handle_scene_tree(params: Dictionary) -> String:
	var root := EditorInterface.get_edited_scene_root()
	if not root:
		return _error_response("EEDITOR_STATE", "No scene currently open in editor",
			"Open a scene in the Godot editor and try again.")
	
	var max_depth: int = params.get("max_depth", 10)
	var max_nodes: int = params.get("max_nodes", 500)
	
	var node_count := [0]  # Use array to pass by reference
	var tree_data := _dump_node(root, root, 0, max_depth, max_nodes, node_count)
	
	return _success_response({
		"root": tree_data,
		"node_count": node_count[0],
		"truncated": node_count[0] >= max_nodes,
	})


func _dump_node(node: Node, scene_root: Node, depth: int, max_depth: int, max_nodes: int, count: Array) -> Dictionary:
	## Recursively dump node tree to dictionary.
	count[0] += 1
	
	var data := {
		"path": _get_scene_path(node, scene_root),
		"name": node.name,
		"type": node.get_class(),
	}
	
	if depth < max_depth and count[0] < max_nodes:
		var children: Array[Dictionary] = []
		for child in node.get_children():
			if count[0] >= max_nodes:
				break
			children.append(_dump_node(child, scene_root, depth + 1, max_depth, max_nodes, count))
		if not children.is_empty():
			data["children"] = children
	
	return data


func _get_scene_path(node: Node, scene_root: Node) -> String:
	## Get a clean path relative to the scene root, prefixed with the root name.
	if node == scene_root:
		return "/root/" + node.name
	
	var path_from_root := scene_root.get_path_to(node)
	return "/root/" + scene_root.name + "/" + str(path_from_root)


func _resolve_node_path(path: String, scene_root: Node) -> Node:
	## Resolve a path like /root/GameRoot/Player to the actual node.
	## Handles both /root/SceneName/... format and relative paths.
	if path.is_empty():
		return null
	
	# Handle /root/SceneName/... format
	if path.begins_with("/root/"):
		var rest := path.substr(6)  # Remove "/root/"
		var parts := rest.split("/")
		if parts.size() == 0:
			return null
		
		# First part should be the scene root name
		if parts[0] == scene_root.name:
			if parts.size() == 1:
				return scene_root
			# Join remaining parts as relative path
			var relative_path := "/".join(parts.slice(1))
			return scene_root.get_node_or_null(relative_path)
		else:
			# Maybe it's a direct child name without scene root prefix
			return scene_root.get_node_or_null(rest)
	
	# Try as relative path from scene root
	return scene_root.get_node_or_null(path)


func _handle_node_inspect(params: Dictionary) -> String:
	var node_path: String = params.get("node_path", "")
	if node_path.is_empty():
		return _error_response("EARG", "node_path is required", "")
	
	var root := EditorInterface.get_edited_scene_root()
	if not root:
		return _error_response("EEDITOR_STATE", "No scene currently open in editor", "")
	
	var node := _resolve_node_path(node_path, root)
	if not node:
		var hint := _get_valid_children_hint(root, node_path)
		return _error_response("ENODE_NOT_FOUND", "Node not found: " + node_path, hint)
	
	var data := {
		"path": _get_scene_path(node, root),
		"name": node.name,
		"type": node.get_class(),
		"script": "",
		"groups": [],
		"signals": [],
		"properties": {},
	}
	
	# Script
	var script := node.get_script()
	if script:
		data["script"] = script.resource_path
	
	# Groups
	for group in node.get_groups():
		data["groups"].append(group)
	
	# Signal connections (outgoing)
	for sig in node.get_signal_list():
		for conn in node.get_signal_connection_list(sig.name):
			data["signals"].append({
				"name": sig.name,
				"target": str(conn.callable.get_object().get_path()) if conn.callable.get_object() else "",
				"method": conn.callable.get_method(),
			})
	
	# Exported/visible properties
	for prop in node.get_property_list():
		if prop.usage & PROPERTY_USAGE_EDITOR:
			var value = node.get(prop.name)
			# Convert to string representation for JSON safety
			data["properties"][prop.name] = _value_to_string(value)
	
	return _success_response(data)


func _handle_node_create(params: Dictionary) -> String:
	var parent_path: String = params.get("parent_path", "")
	var node_type: String = params.get("type", "")
	var node_name: String = params.get("name", "")
	
	if parent_path.is_empty() or node_type.is_empty() or node_name.is_empty():
		return _error_response("EARG", "parent_path, type, and name are required", "")
	
	var root := EditorInterface.get_edited_scene_root()
	if not root:
		return _error_response("EEDITOR_STATE", "No scene currently open in editor", "")
	
	var parent := _resolve_node_path(parent_path, root)
	if not parent:
		var hint := _get_valid_children_hint(root, parent_path)
		return _error_response("ENODE_NOT_FOUND", "Parent node not found: " + parent_path, hint)
	
	# Validate class exists
	if not ClassDB.class_exists(node_type):
		return _error_response("ETYPE_INVALID", "Invalid Godot class: " + node_type,
			"Use a valid Godot class name like Node2D, Sprite2D, CharacterBody2D, etc.")
	
	# Create node with undo/redo support
	var new_node: Node = ClassDB.instantiate(node_type)
	if not new_node:
		return _error_response("ETYPE_INVALID", "Cannot instantiate class: " + node_type, "")
	
	new_node.name = node_name
	
	_undo_redo.create_action("AI: Create Node " + node_name)
	_undo_redo.add_do_method(parent, "add_child", new_node)
	_undo_redo.add_do_method(new_node, "set_owner", root)
	_undo_redo.add_do_reference(new_node)
	_undo_redo.add_undo_method(parent, "remove_child", new_node)
	_undo_redo.commit_action()
	
	return _success_response({
		"created_path": _get_scene_path(new_node, root),
		"type": node_type,
		"name": node_name,
	})


func _handle_node_set_prop(params: Dictionary) -> String:
	var node_path: String = params.get("node_path", "")
	var property: String = params.get("property", "")
	var value_str: String = str(params.get("value", ""))
	
	if node_path.is_empty() or property.is_empty():
		return _error_response("EARG", "node_path and property are required", "")
	
	var root := EditorInterface.get_edited_scene_root()
	if not root:
		return _error_response("EEDITOR_STATE", "No scene currently open in editor", "")
	
	var node := _resolve_node_path(node_path, root)
	if not node:
		var hint := _get_valid_children_hint(root, node_path)
		return _error_response("ENODE_NOT_FOUND", "Node not found: " + node_path, hint)
	
	# Check if property exists
	var current_value = node.get(property)
	var prop_exists := false
	for prop in node.get_property_list():
		if prop.name == property:
			prop_exists = true
			break
	
	if not prop_exists:
		var valid_props := _get_valid_properties(node)
		return _error_response("EPROP_NOT_FOUND", 
			"Property '%s' not found on node type '%s'" % [property, node.get_class()],
			"Valid properties include: " + ", ".join(valid_props.slice(0, 10)))
	
	# Convert value to appropriate type
	var typed_value = _convert_value(value_str, typeof(current_value))
	if typed_value == null and not value_str.is_empty():
		return _error_response("ETYPE_CONVERSION",
			"Cannot convert '%s' to type %s" % [value_str, type_string(typeof(current_value))],
			"Expected format for this type: " + _get_type_format_hint(typeof(current_value)))
	
	# Set property with undo/redo
	_undo_redo.create_action("AI: Set %s.%s" % [node.name, property])
	_undo_redo.add_do_property(node, property, typed_value)
	_undo_redo.add_undo_property(node, property, current_value)
	_undo_redo.commit_action()
	
	return _success_response({
		"ok": true,
		"node_path": node_path,
		"property": property,
		"old_value": _value_to_string(current_value),
		"new_value": _value_to_string(typed_value),
	})


func _handle_node_attach_script(params: Dictionary) -> String:
	var node_path: String = params.get("node_path", "")
	var script_path: String = params.get("script_path", "")
	
	if node_path.is_empty() or script_path.is_empty():
		return _error_response("EARG", "node_path and script_path are required", "")
	
	var root := EditorInterface.get_edited_scene_root()
	if not root:
		return _error_response("EEDITOR_STATE", "No scene currently open in editor", "")
	
	var node := _resolve_node_path(node_path, root)
	if not node:
		var hint := _get_valid_children_hint(root, node_path)
		return _error_response("ENODE_NOT_FOUND", "Node not found: " + node_path, hint)
	
	# Load script
	if not ResourceLoader.exists(script_path):
		return _error_response("ESCRIPT_NOT_FOUND", "Script not found: " + script_path,
			"Ensure the script exists at the specified res:// path.")
	
	var script := load(script_path)
	if not script or not script is Script:
		return _error_response("ESCRIPT_NOT_FOUND", "Failed to load script: " + script_path, "")
	
	var old_script := node.get_script()
	
	_undo_redo.create_action("AI: Attach Script to " + node.name)
	_undo_redo.add_do_method(node, "set_script", script)
	_undo_redo.add_undo_method(node, "set_script", old_script)
	_undo_redo.commit_action()
	
	return _success_response({
		"ok": true,
		"node_path": node_path,
		"script_path": script_path,
	})


func _handle_signal_connect(params: Dictionary) -> String:
	var source_path: String = params.get("source_path", "")
	var signal_name: String = params.get("signal_name", "")
	var target_path: String = params.get("target_path", "")
	var method_name: String = params.get("method_name", "")
	
	if source_path.is_empty() or signal_name.is_empty() or target_path.is_empty() or method_name.is_empty():
		return _error_response("EARG", "source_path, signal_name, target_path, and method_name are required", "")
	
	var root := EditorInterface.get_edited_scene_root()
	if not root:
		return _error_response("EEDITOR_STATE", "No scene currently open in editor", "")
	
	var source := _resolve_node_path(source_path, root)
	if not source:
		return _error_response("ENODE_NOT_FOUND", "Source node not found: " + source_path, "")
	
	var target := _resolve_node_path(target_path, root)
	if not target:
		return _error_response("ENODE_NOT_FOUND", "Target node not found: " + target_path, "")
	
	# Check signal exists
	var has_signal := false
	for sig in source.get_signal_list():
		if sig.name == signal_name:
			has_signal = true
			break
	
	if not has_signal:
		var valid_signals: Array[String] = []
		for sig in source.get_signal_list():
			valid_signals.append(sig.name)
		return _error_response("ESIGNAL_NOT_FOUND",
			"Signal '%s' not found on node type '%s'" % [signal_name, source.get_class()],
			"Valid signals: " + ", ".join(valid_signals.slice(0, 10)))
	
	# Connect signal (note: undo/redo for signals is complex, doing simple connect for now)
	if source.is_connected(signal_name, Callable(target, method_name)):
		return _success_response({
			"ok": true,
			"already_connected": true,
		})
	
	var err := source.connect(signal_name, Callable(target, method_name))
	if err != OK:
		return _error_response("ESIGNAL_CONNECT", "Failed to connect signal: error %d" % err, "")
	
	return _success_response({
		"ok": true,
		"source_path": source_path,
		"signal_name": signal_name,
		"target_path": target_path,
		"method_name": method_name,
	})


func _handle_run_game() -> String:
	EditorInterface.play_main_scene()
	return _success_response({
		"started": true,
	})


func _handle_errors(params: Dictionary) -> String:
	var limit: int = params.get("limit", 50)
	limit = mini(limit, MAX_ERROR_BUFFER)
	
	# Return buffered errors (most recent first)
	var entries: Array[Dictionary] = []
	var start_idx := maxi(0, _error_buffer.size() - limit)
	for i in range(start_idx, _error_buffer.size()):
		entries.append(_error_buffer[i])
	
	return _success_response({
		"entries": entries,
		"total_buffered": _error_buffer.size(),
	})


# -- HELPER FUNCTIONS --

func _get_valid_children_hint(root: Node, invalid_path: String) -> String:
	## Get hint showing valid children when a path is not found.
	var parts := invalid_path.split("/")
	var check_path := ""
	var last_valid: Node = root
	
	for i in range(parts.size()):
		if parts[i].is_empty() or parts[i] == "root":
			continue
		check_path += "/" + parts[i]
		var node := root.get_node_or_null(check_path)
		if node:
			last_valid = node
		else:
			break
	
	var valid_children: Array[String] = []
	for child in last_valid.get_children():
		valid_children.append(child.name)
	
	if valid_children.is_empty():
		return "Node '%s' has no children." % last_valid.name
	
	return "Valid children under '%s': %s" % [str(last_valid.get_path()), ", ".join(valid_children)]


func _get_valid_properties(node: Node) -> Array[String]:
	## Get list of editable property names for a node.
	var props: Array[String] = []
	for prop in node.get_property_list():
		if prop.usage & PROPERTY_USAGE_EDITOR:
			props.append(prop.name)
	return props


func _value_to_string(value: Variant) -> String:
	## Convert a Godot value to a string representation.
	match typeof(value):
		TYPE_VECTOR2:
			return "Vector2(%s, %s)" % [value.x, value.y]
		TYPE_VECTOR3:
			return "Vector3(%s, %s, %s)" % [value.x, value.y, value.z]
		TYPE_COLOR:
			return "Color(%s, %s, %s, %s)" % [value.r, value.g, value.b, value.a]
		TYPE_NIL:
			return "null"
		_:
			return str(value)


func _convert_value(value_str: String, target_type: int) -> Variant:
	## Convert a string value to the target Godot type.
	if value_str.is_empty():
		return null
	
	match target_type:
		TYPE_INT:
			if value_str.is_valid_int():
				return value_str.to_int()
		TYPE_FLOAT:
			if value_str.is_valid_float():
				return value_str.to_float()
		TYPE_BOOL:
			var lower := value_str.to_lower()
			if lower == "true":
				return true
			if lower == "false":
				return false
		TYPE_STRING:
			return value_str
		TYPE_VECTOR2:
			return _parse_vector2(value_str)
		TYPE_VECTOR3:
			return _parse_vector3(value_str)
		TYPE_COLOR:
			return _parse_color(value_str)
	
	# Fallback: try str_to_var for complex types
	var result = str_to_var(value_str)
	if result != null:
		return result
	
	return value_str  # Return as string if all else fails


func _parse_vector2(s: String) -> Variant:
	## Parse "Vector2(x, y)" or "(x, y)" to Vector2.
	var cleaned := s.replace("Vector2", "").replace("(", "").replace(")", "").strip_edges()
	var parts := cleaned.split(",")
	if parts.size() == 2:
		var x := parts[0].strip_edges()
		var y := parts[1].strip_edges()
		if x.is_valid_float() and y.is_valid_float():
			return Vector2(x.to_float(), y.to_float())
	return null


func _parse_vector3(s: String) -> Variant:
	## Parse "Vector3(x, y, z)" or "(x, y, z)" to Vector3.
	var cleaned := s.replace("Vector3", "").replace("(", "").replace(")", "").strip_edges()
	var parts := cleaned.split(",")
	if parts.size() == 3:
		var x := parts[0].strip_edges()
		var y := parts[1].strip_edges()
		var z := parts[2].strip_edges()
		if x.is_valid_float() and y.is_valid_float() and z.is_valid_float():
			return Vector3(x.to_float(), y.to_float(), z.to_float())
	return null


func _parse_color(s: String) -> Variant:
	## Parse color string to Color.
	# Try hex format
	if s.begins_with("#"):
		return Color.html(s)
	# Try Color(r, g, b) or Color(r, g, b, a)
	var cleaned := s.replace("Color", "").replace("(", "").replace(")", "").strip_edges()
	var parts := cleaned.split(",")
	if parts.size() >= 3:
		var r := parts[0].strip_edges()
		var g := parts[1].strip_edges()
		var b := parts[2].strip_edges()
		if r.is_valid_float() and g.is_valid_float() and b.is_valid_float():
			if parts.size() >= 4:
				var a := parts[3].strip_edges()
				if a.is_valid_float():
					return Color(r.to_float(), g.to_float(), b.to_float(), a.to_float())
			return Color(r.to_float(), g.to_float(), b.to_float())
	# Try named color
	return Color(s)


func _get_type_format_hint(type: int) -> String:
	## Get format hint for a type.
	match type:
		TYPE_INT:
			return "Integer, e.g., 42"
		TYPE_FLOAT:
			return "Number, e.g., 3.14"
		TYPE_BOOL:
			return "true or false"
		TYPE_VECTOR2:
			return "Vector2(x, y) or (x, y)"
		TYPE_VECTOR3:
			return "Vector3(x, y, z) or (x, y, z)"
		TYPE_COLOR:
			return "Color(r, g, b), #rrggbb, or color name"
		_:
			return "String value"


# -- RESPONSE HELPERS --

func _success_response(data: Dictionary) -> String:
	return JSON.stringify({
		"status": "success",
		"data": data,
		"error": null,
	})


func _error_response(code: String, message: String, hint: String) -> String:
	return JSON.stringify({
		"status": "error",
		"data": null,
		"error": {
			"code": code,
			"message": message,
			"hint": hint,
		},
	})
