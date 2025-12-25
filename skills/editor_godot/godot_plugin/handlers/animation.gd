## Animation handler for GodotAIBridge - handles AnimationPlayer operations.
extends RefCounted
class_name AnimationHandler


func _init() -> void:
	pass


func handle(action: String, params: Dictionary) -> Dictionary:
	match action:
		"animation_list":
			return _handle_animation_list(params)
		"animation_play":
			return _handle_animation_play(params)
		"animation_stop":
			return _handle_animation_stop(params)
		_:
			return _error_response("EACTION", "AnimationHandler: Unknown action: " + action, "")


func _handle_animation_list(params: Dictionary) -> Dictionary:
	## List animations on an AnimationPlayer node.
	var node_path: String = params.get("node_path", "")

	if node_path.is_empty():
		return _error_response("EARG", "node_path is required", "")

	var root := EditorInterface.get_edited_scene_root()
	if not root:
		return _error_response("EEDITOR_STATE", "No scene currently open in editor", "")

	var node := _resolve_node_path(node_path, root)
	if not node:
		return _error_response("ENODE_NOT_FOUND", "Node not found: " + node_path, "")

	if not node is AnimationPlayer:
		return _error_response("ETYPE_MISMATCH",
			"Node is not an AnimationPlayer: " + node.get_class(),
			"Provide a path to an AnimationPlayer node.")

	var animation_player := node as AnimationPlayer
	var animations: Array[Dictionary] = []

	for anim_name in animation_player.get_animation_list():
		var anim := animation_player.get_animation(anim_name)
		animations.append({
			"name": anim_name,
			"length": anim.length if anim else 0.0,
			"loop_mode": anim.loop_mode if anim else 0,
		})

	return _success_response({
		"node_path": _get_scene_path(node, root),
		"animations": animations,
		"count": animations.size(),
		"current": animation_player.current_animation,
		"is_playing": animation_player.is_playing(),
	})


func _handle_animation_play(params: Dictionary) -> Dictionary:
	## Play an animation on an AnimationPlayer node.
	var node_path: String = params.get("node_path", "")
	var animation_name: String = params.get("animation", "")
	var blend_time: float = params.get("blend_time", -1.0)
	var playback_speed: float = params.get("playback_speed", 1.0)
	var from_end: bool = params.get("from_end", false)

	if node_path.is_empty():
		return _error_response("EARG", "node_path is required", "")
	if animation_name.is_empty():
		return _error_response("EARG", "animation is required", "")

	var root := EditorInterface.get_edited_scene_root()
	if not root:
		return _error_response("EEDITOR_STATE", "No scene currently open in editor", "")

	var node := _resolve_node_path(node_path, root)
	if not node:
		return _error_response("ENODE_NOT_FOUND", "Node not found: " + node_path, "")

	if not node is AnimationPlayer:
		return _error_response("ETYPE_MISMATCH",
			"Node is not an AnimationPlayer: " + node.get_class(),
			"Provide a path to an AnimationPlayer node.")

	var animation_player := node as AnimationPlayer

	# Check animation exists
	if not animation_player.has_animation(animation_name):
		var available := animation_player.get_animation_list()
		return _error_response("EANIMATION_NOT_FOUND",
			"Animation not found: " + animation_name,
			"Available animations: " + ", ".join(available))

	# Set playback speed
	animation_player.speed_scale = playback_speed

	# Play the animation
	if blend_time >= 0:
		if from_end:
			animation_player.play_backwards(animation_name, blend_time)
		else:
			animation_player.play(animation_name, blend_time)
	else:
		if from_end:
			animation_player.play_backwards(animation_name)
		else:
			animation_player.play(animation_name)

	return _success_response({
		"ok": true,
		"node_path": _get_scene_path(node, root),
		"animation": animation_name,
		"playback_speed": playback_speed,
		"from_end": from_end,
	})


func _handle_animation_stop(params: Dictionary) -> Dictionary:
	## Stop animation on an AnimationPlayer node.
	var node_path: String = params.get("node_path", "")

	if node_path.is_empty():
		return _error_response("EARG", "node_path is required", "")

	var root := EditorInterface.get_edited_scene_root()
	if not root:
		return _error_response("EEDITOR_STATE", "No scene currently open in editor", "")

	var node := _resolve_node_path(node_path, root)
	if not node:
		return _error_response("ENODE_NOT_FOUND", "Node not found: " + node_path, "")

	if not node is AnimationPlayer:
		return _error_response("ETYPE_MISMATCH",
			"Node is not an AnimationPlayer: " + node.get_class(),
			"Provide a path to an AnimationPlayer node.")

	var animation_player := node as AnimationPlayer
	var was_playing := animation_player.is_playing()
	var current := animation_player.current_animation

	animation_player.stop()

	return _success_response({
		"ok": true,
		"node_path": _get_scene_path(node, root),
		"was_playing": was_playing,
		"stopped_animation": current,
	})


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
