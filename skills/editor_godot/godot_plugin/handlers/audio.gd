## Audio handler for GodotAIBridge - handles AudioStreamPlayer operations.
extends RefCounted
class_name AudioHandler


func _init() -> void:
	pass


func handle(action: String, params: Dictionary) -> Dictionary:
	match action:
		"audio_play":
			return _handle_audio_play(params)
		"audio_stop":
			return _handle_audio_stop(params)
		_:
			return _error_response("EACTION", "AudioHandler: Unknown action: " + action, "")


func _handle_audio_play(params: Dictionary) -> Dictionary:
	## Play audio on an AudioStreamPlayer node.
	var node_path: String = params.get("node_path", "")
	var audio_path: String = params.get("audio_path", "")
	var volume_db: float = params.get("volume_db", 0.0)
	var pitch_scale: float = params.get("pitch_scale", 1.0)
	var bus: String = params.get("bus", "Master")

	if node_path.is_empty():
		return _error_response("EARG", "node_path is required", "")

	var root := EditorInterface.get_edited_scene_root()
	if not root:
		return _error_response("EEDITOR_STATE", "No scene currently open in editor", "")

	var node := _resolve_node_path(node_path, root)
	if not node:
		return _error_response("ENODE_NOT_FOUND", "Node not found: " + node_path, "")

	# Check if it's an audio player (2D, 3D, or base)
	var is_audio_player := false
	if node is AudioStreamPlayer or node is AudioStreamPlayer2D or node is AudioStreamPlayer3D:
		is_audio_player = true

	if not is_audio_player:
		return _error_response("ETYPE_MISMATCH",
			"Node is not an AudioStreamPlayer: " + node.get_class(),
			"Provide a path to an AudioStreamPlayer, AudioStreamPlayer2D, or AudioStreamPlayer3D node.")

	# Load audio stream if provided
	if not audio_path.is_empty():
		if not audio_path.begins_with("res://"):
			audio_path = "res://" + audio_path.lstrip("/")

		if not ResourceLoader.exists(audio_path):
			return _error_response("ERESOURCE_NOT_FOUND", "Audio resource not found: " + audio_path, "")

		var stream := load(audio_path)
		if not stream or not stream is AudioStream:
			return _error_response("ERESOURCE_INVALID", "Not a valid audio stream: " + audio_path, "")

		node.stream = stream

	# Apply settings
	node.volume_db = volume_db
	node.pitch_scale = pitch_scale
	node.bus = bus

	# Play
	node.play()

	return _success_response({
		"ok": true,
		"node_path": _get_scene_path(node, root),
		"audio_path": audio_path if not audio_path.is_empty() else (node.stream.resource_path if node.stream else ""),
		"volume_db": volume_db,
		"pitch_scale": pitch_scale,
		"bus": bus,
	})


func _handle_audio_stop(params: Dictionary) -> Dictionary:
	## Stop audio on an AudioStreamPlayer node.
	var node_path: String = params.get("node_path", "")

	if node_path.is_empty():
		return _error_response("EARG", "node_path is required", "")

	var root := EditorInterface.get_edited_scene_root()
	if not root:
		return _error_response("EEDITOR_STATE", "No scene currently open in editor", "")

	var node := _resolve_node_path(node_path, root)
	if not node:
		return _error_response("ENODE_NOT_FOUND", "Node not found: " + node_path, "")

	# Check if it's an audio player
	var is_audio_player := false
	if node is AudioStreamPlayer or node is AudioStreamPlayer2D or node is AudioStreamPlayer3D:
		is_audio_player = true

	if not is_audio_player:
		return _error_response("ETYPE_MISMATCH",
			"Node is not an AudioStreamPlayer: " + node.get_class(),
			"Provide a path to an AudioStreamPlayer, AudioStreamPlayer2D, or AudioStreamPlayer3D node.")

	var was_playing := node.playing

	node.stop()

	return _success_response({
		"ok": true,
		"node_path": _get_scene_path(node, root),
		"was_playing": was_playing,
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
