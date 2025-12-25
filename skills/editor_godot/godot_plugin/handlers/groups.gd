## Groups handler for GodotAIBridge - handles node group operations.
extends RefCounted
class_name GroupsHandler


func _init() -> void:
	pass


func handle(action: String, params: Dictionary) -> Dictionary:
	match action:
		"group_add":
			return _handle_group_add(params)
		"group_remove":
			return _handle_group_remove(params)
		"group_list":
			return _handle_group_list(params)
		_:
			return _error_response("EARG", "GroupsHandler: Unknown action: " + action, "")


func _handle_group_add(params: Dictionary) -> Dictionary:
	## Add a node to a group.
	var node_path: String = params.get("node_path", "")
	var group_name: String = params.get("group_name", "")

	if node_path.is_empty():
		return _error_response("EARG", "node_path is required", "")
	if group_name.is_empty():
		return _error_response("EARG", "group_name is required", "")

	var root := EditorInterface.get_edited_scene_root()
	if not root:
		return _error_response("EEDITOR_STATE", "No scene currently open in editor", "")

	var node := _resolve_node_path(node_path, root)
	if not node:
		return _error_response("ENOTFOUND", "Node not found: " + node_path, "")

	# Check if already in group
	if node.is_in_group(group_name):
		return _success_response({
			"ok": true,
			"already_in_group": true,
			"node_path": _get_scene_path(node, root),
			"group": group_name,
		})

	node.add_to_group(group_name, true)  # persistent = true

	return _success_response({
		"ok": true,
		"node_path": _get_scene_path(node, root),
		"group": group_name,
	})


func _handle_group_remove(params: Dictionary) -> Dictionary:
	## Remove a node from a group.
	var node_path: String = params.get("node_path", "")
	var group_name: String = params.get("group_name", "")

	if node_path.is_empty():
		return _error_response("EARG", "node_path is required", "")
	if group_name.is_empty():
		return _error_response("EARG", "group_name is required", "")

	var root := EditorInterface.get_edited_scene_root()
	if not root:
		return _error_response("EEDITOR_STATE", "No scene currently open in editor", "")

	var node := _resolve_node_path(node_path, root)
	if not node:
		return _error_response("ENOTFOUND", "Node not found: " + node_path, "")

	# Check if in group
	if not node.is_in_group(group_name):
		return _success_response({
			"ok": true,
			"was_not_in_group": true,
			"node_path": _get_scene_path(node, root),
			"group": group_name,
		})

	node.remove_from_group(group_name)

	return _success_response({
		"ok": true,
		"node_path": _get_scene_path(node, root),
		"group": group_name,
	})


func _handle_group_list(params: Dictionary) -> Dictionary:
	## List groups for a node, or list all nodes in a group.
	var node_path: String = params.get("node_path", "")
	var group_name: String = params.get("group_name", "")

	var root := EditorInterface.get_edited_scene_root()
	if not root:
		return _error_response("EEDITOR_STATE", "No scene currently open in editor", "")

	if not node_path.is_empty():
		# List groups for a specific node
		var node := _resolve_node_path(node_path, root)
		if not node:
			return _error_response("ENOTFOUND", "Node not found: " + node_path, "")

		var groups: Array[String] = []
		for group in node.get_groups():
			groups.append(group)

		return _success_response({
			"node_path": _get_scene_path(node, root),
			"groups": groups,
			"count": groups.size(),
		})
	elif not group_name.is_empty():
		# List all nodes in a group
		var nodes_in_group: Array[Dictionary] = []
		_find_nodes_in_group(root, root, group_name, nodes_in_group)

		return _success_response({
			"group": group_name,
			"nodes": nodes_in_group,
			"count": nodes_in_group.size(),
		})
	else:
		# List all unique groups in the scene
		var all_groups: Dictionary = {}  # Use dict as set
		_collect_all_groups(root, all_groups)

		var groups: Array[String] = []
		for group in all_groups.keys():
			groups.append(group)

		return _success_response({
			"groups": groups,
			"count": groups.size(),
		})


func _find_nodes_in_group(node: Node, scene_root: Node, group_name: String, results: Array[Dictionary]) -> void:
	if node.is_in_group(group_name):
		results.append({
			"path": _get_scene_path(node, scene_root),
			"name": node.name,
			"type": node.get_class(),
		})

	for child in node.get_children():
		_find_nodes_in_group(child, scene_root, group_name, results)


func _collect_all_groups(node: Node, groups: Dictionary) -> void:
	for group in node.get_groups():
		groups[group] = true

	for child in node.get_children():
		_collect_all_groups(child, groups)


# -- HELPER FUNCTIONS --

func _get_scene_path(node: Node, scene_root: Node) -> String:
	if node == scene_root:
		return "/root/" + node.name
	var path_from_root := scene_root.get_path_to(node)
	return "/root/" + scene_root.name + "/" + str(path_from_root)


func _resolve_node_path(path: String, scene_root: Node) -> Node:
	if path.is_empty():
		return null
	if path.begins_with("/root/"):
		var rest := path.substr(6)
		var parts := rest.split("/")
		if parts.size() == 0:
			return null
		if parts[0] == scene_root.name:
			if parts.size() == 1:
				return scene_root
			var relative_path := "/".join(parts.slice(1))
			return scene_root.get_node_or_null(relative_path)
		else:
			return scene_root.get_node_or_null(rest)
	return scene_root.get_node_or_null(path)


func _success_response(data: Dictionary) -> Dictionary:
	return {"status": "success", "data": data, "error": null}


func _error_response(code: String, message: String, hint: String) -> Dictionary:
	return {"status": "error", "data": null, "error": {"code": code, "message": message, "hint": hint}}
