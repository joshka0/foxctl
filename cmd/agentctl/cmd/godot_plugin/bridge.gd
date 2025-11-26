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
		"node_delete":
			return _handle_node_delete(params)
		"node_rename":
			return _handle_node_rename(params)
		"node_reparent":
			return _handle_node_reparent(params)
		"node_set_prop":
			return _handle_node_set_prop(params)
		"node_attach_script":
			return _handle_node_attach_script(params)
		"signal_connect":
			return _handle_signal_connect(params)
		"class_info":
			return _handle_class_info(params)
		"ensure_node":
			return _handle_ensure_node(params)
		"scene_save":
			return _handle_scene_save()
		"scene_list":
			return _handle_scene_list(params)
		"scene_open":
			return _handle_scene_open(params)
		"scene_instance":
			return _handle_scene_instance(params)
		"resource_list":
			return _handle_resource_list(params)
		"run_game":
			return _handle_run_game()
		"stop_game":
			return _handle_stop_game()
		"errors":
			return _handle_errors(params)
		_:
			return _error_response("EACTION", "Unknown action: " + action, 
				"Valid actions: ping, scene_tree, node_inspect, node_create, node_delete, node_rename, node_reparent, node_set_prop, node_attach_script, signal_connect, class_info, ensure_node, scene_save, scene_list, scene_open, scene_instance, resource_list, run_game, stop_game, errors")


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


func _handle_node_delete(params: Dictionary) -> String:
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
	
	# Cannot delete scene root
	if node == root:
		return _error_response("EARG", "Cannot delete scene root node", "")
	
	var parent := node.get_parent()
	var node_name := node.name
	var node_index := node.get_index()
	
	_undo_redo.create_action("AI: Delete Node " + node_name)
	_undo_redo.add_do_method(parent, "remove_child", node)
	_undo_redo.add_undo_method(parent, "add_child", node)
	_undo_redo.add_undo_method(parent, "move_child", node, node_index)
	_undo_redo.add_undo_method(node, "set_owner", root)
	_undo_redo.add_undo_reference(node)
	_undo_redo.commit_action()
	
	return _success_response({
		"ok": true,
		"deleted_path": node_path,
		"name": node_name,
	})


func _handle_node_rename(params: Dictionary) -> String:
	var node_path: String = params.get("node_path", "")
	var new_name: String = params.get("new_name", "")
	
	if node_path.is_empty() or new_name.is_empty():
		return _error_response("EARG", "node_path and new_name are required", "")
	
	var root := EditorInterface.get_edited_scene_root()
	if not root:
		return _error_response("EEDITOR_STATE", "No scene currently open in editor", "")
	
	var node := _resolve_node_path(node_path, root)
	if not node:
		var hint := _get_valid_children_hint(root, node_path)
		return _error_response("ENODE_NOT_FOUND", "Node not found: " + node_path, hint)
	
	var old_name := node.name
	
	_undo_redo.create_action("AI: Rename Node %s to %s" % [old_name, new_name])
	_undo_redo.add_do_property(node, "name", new_name)
	_undo_redo.add_undo_property(node, "name", old_name)
	_undo_redo.commit_action()
	
	return _success_response({
		"ok": true,
		"old_name": old_name,
		"new_name": new_name,
		"new_path": _get_scene_path(node, root),
	})


func _handle_node_reparent(params: Dictionary) -> String:
	var node_path: String = params.get("node_path", "")
	var new_parent_path: String = params.get("new_parent_path", "")
	
	if node_path.is_empty() or new_parent_path.is_empty():
		return _error_response("EARG", "node_path and new_parent_path are required", "")
	
	var root := EditorInterface.get_edited_scene_root()
	if not root:
		return _error_response("EEDITOR_STATE", "No scene currently open in editor", "")
	
	var node := _resolve_node_path(node_path, root)
	if not node:
		return _error_response("ENODE_NOT_FOUND", "Node not found: " + node_path, "")
	
	var new_parent := _resolve_node_path(new_parent_path, root)
	if not new_parent:
		return _error_response("ENODE_NOT_FOUND", "New parent not found: " + new_parent_path, "")
	
	# Cannot reparent to self or descendant
	if new_parent == node or node.is_ancestor_of(new_parent):
		return _error_response("EARG", "Cannot reparent node to itself or its descendant", "")
	
	var old_parent := node.get_parent()
	var old_index := node.get_index()
	
	_undo_redo.create_action("AI: Reparent Node " + node.name)
	_undo_redo.add_do_method(old_parent, "remove_child", node)
	_undo_redo.add_do_method(new_parent, "add_child", node)
	_undo_redo.add_do_method(node, "set_owner", root)
	_undo_redo.add_undo_method(new_parent, "remove_child", node)
	_undo_redo.add_undo_method(old_parent, "add_child", node)
	_undo_redo.add_undo_method(old_parent, "move_child", node, old_index)
	_undo_redo.add_undo_method(node, "set_owner", root)
	_undo_redo.commit_action()
	
	return _success_response({
		"ok": true,
		"new_path": _get_scene_path(node, root),
	})


func _handle_class_info(params: Dictionary) -> String:
	var godot_class: String = params.get("class_name", "")
	
	if godot_class.is_empty():
		return _error_response("EARG", "class_name is required", "")
	
	if not ClassDB.class_exists(godot_class):
		return _error_response("ETYPE_INVALID", "Class not found: " + godot_class,
			"Use a valid Godot class name like Node2D, Sprite2D, CharacterBody2D, etc.")
	
	var parent_class := ClassDB.get_parent_class(godot_class)
	
	# Get methods (limit to avoid huge output)
	var methods: Array[String] = []
	for method in ClassDB.class_get_method_list(godot_class, true):
		if methods.size() >= 50:
			break
		methods.append(method.name)
	
	# Get properties
	var properties: Array[Dictionary] = []
	for prop in ClassDB.class_get_property_list(godot_class, true):
		if properties.size() >= 50:
			break
		properties.append({
			"name": prop.name,
			"type": type_string(prop.type),
		})
	
	# Get signals
	var signals: Array[String] = []
	for sig in ClassDB.class_get_signal_list(godot_class, true):
		signals.append(sig.name)
	
	return _success_response({
		"class_name": godot_class,
		"parent_class": parent_class,
		"methods": methods,
		"properties": properties,
		"signals": signals,
	})


func _handle_ensure_node(params: Dictionary) -> String:
	## Idempotently ensure a node exists at the given path with the given type.
	## If the node exists and type matches, optionally update properties.
	## If the node doesn't exist, create it.
	var target_path: String = params.get("path", "")
	var node_type: String = params.get("type", "")
	var props: Dictionary = params.get("props", {})
	var if_exists: String = params.get("if_exists", "update")
	
	if target_path.is_empty() or node_type.is_empty():
		return _error_response("EARG", "path and type are required", "")
	
	var root := EditorInterface.get_edited_scene_root()
	if not root:
		return _error_response("EEDITOR_STATE", "No scene currently open in editor", "")
	
	# Try to find existing node
	var existing := _resolve_node_path(target_path, root)
	
	if existing:
		# Node exists - check type
		if existing.get_class() != node_type:
			# Check if it's a subclass (e.g., CharacterBody2D is a Node2D)
			if not ClassDB.is_parent_class(existing.get_class(), node_type):
				return _error_response("ETYPE_MISMATCH",
					"Node exists but type mismatch: expected '%s', found '%s'" % [node_type, existing.get_class()],
					"Use node_delete first if you want to replace the node with a different type.")
		
		# Handle based on if_exists policy
		match if_exists:
			"error":
				return _error_response("ENODE_EXISTS",
					"Node already exists at: " + target_path,
					"Use if_exists='update' or 'ignore' to handle existing nodes.")
			"ignore":
				return _success_response({
					"status": "exists",
					"path": _get_scene_path(existing, root),
					"type": existing.get_class(),
					"created": false,
					"updated_props": [],
				})
			"update", _:
				# Update properties if provided
				var updated_props: Array[String] = []
				if not props.is_empty():
					_undo_redo.create_action("AI: Ensure Node %s props" % existing.name)
					for prop_name in props:
						var value_str: String = str(props[prop_name])
						var current_value = existing.get(prop_name)
						
						# Check property exists
						var prop_exists := false
						for prop in existing.get_property_list():
							if prop.name == prop_name:
								prop_exists = true
								break
						
						if not prop_exists:
							_undo_redo.commit_action()  # Commit empty action to avoid issues
							var valid_props := _get_valid_properties(existing)
							return _error_response("EPROP_NOT_FOUND",
								"Property '%s' not found on node type '%s'" % [prop_name, existing.get_class()],
								"Valid properties include: " + ", ".join(valid_props.slice(0, 10)))
						
						var typed_value = _convert_value(value_str, typeof(current_value))
						if typed_value == null and not value_str.is_empty():
							_undo_redo.commit_action()
							return _error_response("ETYPE_CONVERSION",
								"Cannot convert '%s' to type %s" % [value_str, type_string(typeof(current_value))],
								"Expected format: " + _get_type_format_hint(typeof(current_value)))
						
						_undo_redo.add_do_property(existing, prop_name, typed_value)
						_undo_redo.add_undo_property(existing, prop_name, current_value)
						updated_props.append(prop_name)
					
					_undo_redo.commit_action()
				
				return _success_response({
					"status": "exists",
					"path": _get_scene_path(existing, root),
					"type": existing.get_class(),
					"created": false,
					"updated_props": updated_props,
				})
	
	# Node doesn't exist - create it
	# Parse path to get parent and name
	var path_parts := target_path.split("/")
	if path_parts.size() < 3:  # Need at least /root/SceneName/NodeName
		return _error_response("EARG", "Invalid path format: " + target_path,
			"Path must be like /root/SceneName/NodeName")
	
	var node_name := path_parts[-1]
	var parent_path := "/".join(path_parts.slice(0, -1))
	
	var parent := _resolve_node_path(parent_path, root)
	if not parent:
		return _error_response("ENODE_NOT_FOUND", "Parent node not found: " + parent_path,
			"Create parent nodes first or check the path.")
	
	# Validate class exists
	if not ClassDB.class_exists(node_type):
		return _error_response("ETYPE_INVALID", "Invalid Godot class: " + node_type,
			"Use a valid Godot class name like Node2D, Sprite2D, CharacterBody2D, etc.")
	
	# Create node
	var new_node: Node = ClassDB.instantiate(node_type)
	if not new_node:
		return _error_response("ETYPE_INVALID", "Cannot instantiate class: " + node_type, "")
	
	new_node.name = node_name
	
	# Build undo/redo action for creation + property setting
	_undo_redo.create_action("AI: Ensure Node " + node_name)
	_undo_redo.add_do_method(parent, "add_child", new_node)
	_undo_redo.add_do_method(new_node, "set_owner", root)
	_undo_redo.add_do_reference(new_node)
	_undo_redo.add_undo_method(parent, "remove_child", new_node)
	
	# Apply properties during creation
	var props_applied: Array[String] = []
	for prop_name in props:
		var value_str: String = str(props[prop_name])
		
		# We need to check if property exists on the class
		var prop_info: Dictionary = {}
		for prop in ClassDB.class_get_property_list(node_type, true):
			if prop.name == prop_name:
				prop_info = prop
				break
		
		if prop_info.is_empty():
			# Property might be inherited or dynamic, try to get default
			var default_value = new_node.get(prop_name)
			if default_value == null:
				_undo_redo.commit_action()
				new_node.queue_free()
				return _error_response("EPROP_NOT_FOUND",
					"Property '%s' not found on class '%s'" % [prop_name, node_type], "")
			
			var typed_value = _convert_value(value_str, typeof(default_value))
			if typed_value != null or value_str.is_empty():
				_undo_redo.add_do_property(new_node, prop_name, typed_value)
				props_applied.append(prop_name)
		else:
			var typed_value = _convert_value(value_str, prop_info.type)
			if typed_value != null or value_str.is_empty():
				_undo_redo.add_do_property(new_node, prop_name, typed_value)
				props_applied.append(prop_name)
	
	_undo_redo.commit_action()
	
	return _success_response({
		"status": "created",
		"path": _get_scene_path(new_node, root),
		"type": node_type,
		"created": true,
		"props_applied": props_applied,
	})


func _handle_scene_save() -> String:
	var root := EditorInterface.get_edited_scene_root()
	if not root:
		return _error_response("EEDITOR_STATE", "No scene currently open in editor", "")
	
	EditorInterface.save_scene()
	
	var scene_path := root.scene_file_path
	return _success_response({
		"ok": true,
		"scene_path": scene_path,
	})


func _handle_scene_list(params: Dictionary) -> String:
	## List all scenes in the project, optionally filtered by path.
	var search_path: String = params.get("path", "res://")
	var max_results: int = params.get("max_results", 100)
	var recursive: bool = params.get("recursive", true)
	
	if not search_path.begins_with("res://"):
		search_path = "res://" + search_path.lstrip("/")
	
	var scenes: Array[Dictionary] = []
	_find_scenes_recursive(search_path, scenes, max_results, recursive)
	
	return _success_response({
		"scenes": scenes,
		"search_path": search_path,
		"count": scenes.size(),
		"truncated": scenes.size() >= max_results,
	})


func _find_scenes_recursive(path: String, scenes: Array[Dictionary], max_results: int, recursive: bool) -> void:
	## Recursively find .tscn files.
	var dir := DirAccess.open(path)
	if not dir:
		return
	
	dir.list_dir_begin()
	var file_name := dir.get_next()
	while file_name != "" and scenes.size() < max_results:
		if file_name.begins_with("."):
			file_name = dir.get_next()
			continue
		
		var full_path := path.path_join(file_name)
		
		if dir.current_is_dir():
			if recursive:
				_find_scenes_recursive(full_path, scenes, max_results, recursive)
		elif file_name.ends_with(".tscn"):
			scenes.append({
				"path": full_path,
				"name": file_name.get_basename(),
			})
		
		file_name = dir.get_next()
	dir.list_dir_end()


func _handle_scene_open(params: Dictionary) -> String:
	## Open a scene in the editor.
	var scene_path: String = params.get("path", "")
	
	if scene_path.is_empty():
		return _error_response("EARG", "path is required", "")
	
	if not scene_path.begins_with("res://"):
		scene_path = "res://" + scene_path.lstrip("/")
	
	if not ResourceLoader.exists(scene_path):
		return _error_response("ESCENE_NOT_FOUND", "Scene not found: " + scene_path,
			"Ensure the scene file exists at the specified res:// path.")
	
	# Check if scene is a valid PackedScene
	var resource := load(scene_path)
	if not resource or not resource is PackedScene:
		return _error_response("ESCENE_INVALID", "Not a valid scene file: " + scene_path, "")
	
	# Open the scene
	EditorInterface.open_scene_from_path(scene_path)
	
	# Get the new root after opening
	var new_root := EditorInterface.get_edited_scene_root()
	
	return _success_response({
		"ok": true,
		"scene_path": scene_path,
		"root_name": new_root.name if new_root else "",
		"root_type": new_root.get_class() if new_root else "",
	})


func _handle_scene_instance(params: Dictionary) -> String:
	## Instance a scene as a child of a node.
	var scene_path: String = params.get("scene_path", "")
	var parent_path: String = params.get("parent_path", "")
	var instance_name: String = params.get("name", "")
	
	if scene_path.is_empty() or parent_path.is_empty():
		return _error_response("EARG", "scene_path and parent_path are required", "")
	
	if not scene_path.begins_with("res://"):
		scene_path = "res://" + scene_path.lstrip("/")
	
	var root := EditorInterface.get_edited_scene_root()
	if not root:
		return _error_response("EEDITOR_STATE", "No scene currently open in editor", "")
	
	var parent := _resolve_node_path(parent_path, root)
	if not parent:
		return _error_response("ENODE_NOT_FOUND", "Parent node not found: " + parent_path, "")
	
	# Load the scene
	if not ResourceLoader.exists(scene_path):
		return _error_response("ESCENE_NOT_FOUND", "Scene not found: " + scene_path, "")
	
	var packed_scene: PackedScene = load(scene_path)
	if not packed_scene:
		return _error_response("ESCENE_INVALID", "Failed to load scene: " + scene_path, "")
	
	# Instance the scene
	var instance := packed_scene.instantiate()
	if not instance:
		return _error_response("ESCENE_INVALID", "Failed to instantiate scene: " + scene_path, "")
	
	# Set name if provided
	if not instance_name.is_empty():
		instance.name = instance_name
	
	# Add with undo/redo
	_undo_redo.create_action("AI: Instance Scene " + instance.name)
	_undo_redo.add_do_method(parent, "add_child", instance)
	_undo_redo.add_do_method(instance, "set_owner", root)
	_undo_redo.add_do_reference(instance)
	_undo_redo.add_undo_method(parent, "remove_child", instance)
	_undo_redo.commit_action()
	
	return _success_response({
		"ok": true,
		"instance_path": _get_scene_path(instance, root),
		"instance_name": instance.name,
		"scene_path": scene_path,
	})


func _handle_resource_list(params: Dictionary) -> String:
	var path: String = params.get("path", "res://")
	var pattern: String = params.get("pattern", "")
	var max_results: int = params.get("max_results", 100)
	
	if not path.begins_with("res://"):
		path = "res://" + path.lstrip("/")
	
	var dir := DirAccess.open(path)
	if not dir:
		return _error_response("EARG", "Cannot open directory: " + path, "")
	
	var files: Array[Dictionary] = []
	var dirs: Array[String] = []
	
	dir.list_dir_begin()
	var file_name := dir.get_next()
	while file_name != "" and files.size() + dirs.size() < max_results:
		if file_name.begins_with("."):
			file_name = dir.get_next()
			continue
		
		var full_path := path.path_join(file_name)
		
		if dir.current_is_dir():
			dirs.append(file_name)
		else:
			# Apply pattern filter if specified
			if pattern.is_empty() or file_name.match(pattern):
				files.append({
					"name": file_name,
					"path": full_path,
				})
		
		file_name = dir.get_next()
	dir.list_dir_end()
	
	return _success_response({
		"path": path,
		"directories": dirs,
		"files": files,
		"truncated": files.size() + dirs.size() >= max_results,
	})


func _handle_run_game() -> String:
	EditorInterface.play_main_scene()
	return _success_response({
		"started": true,
	})


func _handle_stop_game() -> String:
	EditorInterface.stop_playing_scene()
	return _success_response({
		"stopped": true,
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
