## Resources handler for GodotAIBridge - handles scenes, resources, and camera operations.
extends RefCounted
class_name ResourcesHandler

var _undo_redo: EditorUndoRedoManager
var _camera_bookmarks: Dictionary = {}  # name -> {position, rotation, zoom}


func _init(undo_redo: EditorUndoRedoManager) -> void:
	_undo_redo = undo_redo


func handle(action: String, params: Dictionary) -> Dictionary:
	match action:
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
		"search_resources":
			return _handle_search_resources(params)
		"resource_references":
			return _handle_resource_references(params)
		"camera_save":
			return _handle_camera_save(params)
		"camera_restore":
			return _handle_camera_restore(params)
		"camera_list":
			return _handle_camera_list()
		_:
			return _error_response("EACTION", "ResourcesHandler: Unknown action: " + action, "")


# -- SCENE HANDLERS --

func _handle_scene_save() -> Dictionary:
	var root := EditorInterface.get_edited_scene_root()
	if not root:
		return _error_response("EEDITOR_STATE", "No scene currently open in editor", "")

	EditorInterface.save_scene()

	var scene_path := root.scene_file_path
	return _success_response({
		"ok": true,
		"scene_path": scene_path,
	})


func _handle_scene_list(params: Dictionary) -> Dictionary:
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


func _handle_scene_open(params: Dictionary) -> Dictionary:
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


func _handle_scene_instance(params: Dictionary) -> Dictionary:
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


# -- RESOURCE HANDLERS --

func _handle_resource_list(params: Dictionary) -> Dictionary:
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


func _handle_search_resources(params: Dictionary) -> Dictionary:
	## Search for resources by type (e.g., PackedScene, Texture2D, Script).
	var resource_type: String = params.get("type", "")
	var search_path: String = params.get("path", "res://")
	var name_pattern: String = params.get("name", "")
	var max_results: int = params.get("max_results", 50)

	if resource_type.is_empty():
		return _error_response("EARG", "type is required (e.g., PackedScene, Texture2D, Script)", "")

	if not search_path.begins_with("res://"):
		search_path = "res://" + search_path.lstrip("/")

	var results: Array[Dictionary] = []
	_search_resources_recursive(search_path, resource_type, name_pattern, results, max_results)

	return _success_response({
		"results": results,
		"count": results.size(),
		"truncated": results.size() >= max_results,
		"filters": {
			"type": resource_type,
			"path": search_path,
			"name": name_pattern,
		},
	})


func _search_resources_recursive(path: String, resource_type: String, name_pattern: String, results: Array[Dictionary], max_results: int) -> void:
	## Recursively search for resources of a specific type.
	if results.size() >= max_results:
		return

	var dir := DirAccess.open(path)
	if not dir:
		return

	dir.list_dir_begin()
	var file_name := dir.get_next()
	while file_name != "" and results.size() < max_results:
		if file_name.begins_with("."):
			file_name = dir.get_next()
			continue

		var full_path := path.path_join(file_name)

		if dir.current_is_dir():
			_search_resources_recursive(full_path, resource_type, name_pattern, results, max_results)
		else:
			# Check if name matches pattern
			if not name_pattern.is_empty() and not file_name.matchn(name_pattern):
				file_name = dir.get_next()
				continue

			# Try to determine resource type without fully loading
			var matches := false
			match resource_type.to_lower():
				"packedscene", "scene":
					matches = file_name.ends_with(".tscn") or file_name.ends_with(".scn")
				"script", "gdscript":
					matches = file_name.ends_with(".gd")
				"texture2d", "texture":
					matches = file_name.ends_with(".png") or file_name.ends_with(".jpg") or file_name.ends_with(".webp") or file_name.ends_with(".svg")
				"audiostreammp3", "audiostream", "audio":
					matches = file_name.ends_with(".mp3") or file_name.ends_with(".ogg") or file_name.ends_with(".wav")
				"shader":
					matches = file_name.ends_with(".gdshader") or file_name.ends_with(".shader")
				"material":
					matches = file_name.ends_with(".material") or file_name.ends_with(".tres")
				"resource", "tres":
					matches = file_name.ends_with(".tres")
				_:
					# For other types, try to load and check
					if ResourceLoader.exists(full_path):
						var res := load(full_path)
						if res and res.get_class() == resource_type:
							matches = true

			if matches:
				results.append({
					"path": full_path,
					"name": file_name,
					"type": resource_type,
				})

		file_name = dir.get_next()
	dir.list_dir_end()


func _handle_resource_references(params: Dictionary) -> Dictionary:
	## Find scenes that reference a given resource.
	var resource_path: String = params.get("path", "")
	var max_results: int = params.get("max_results", 50)

	if resource_path.is_empty():
		return _error_response("EARG", "path is required", "")

	if not resource_path.begins_with("res://"):
		resource_path = "res://" + resource_path.lstrip("/")

	if not ResourceLoader.exists(resource_path):
		return _error_response("ERESOURCE_NOT_FOUND", "Resource not found: " + resource_path, "")

	var references: Array[Dictionary] = []
	_find_resource_references_recursive("res://", resource_path, references, max_results)

	return _success_response({
		"resource_path": resource_path,
		"references": references,
		"count": references.size(),
		"truncated": references.size() >= max_results,
	})


func _find_resource_references_recursive(search_path: String, target_resource: String, references: Array[Dictionary], max_results: int) -> void:
	## Recursively search for scenes/resources that reference the target.
	if references.size() >= max_results:
		return

	var dir := DirAccess.open(search_path)
	if not dir:
		return

	dir.list_dir_begin()
	var file_name := dir.get_next()
	while file_name != "" and references.size() < max_results:
		if file_name.begins_with("."):
			file_name = dir.get_next()
			continue

		var full_path := search_path.path_join(file_name)

		if dir.current_is_dir():
			_find_resource_references_recursive(full_path, target_resource, references, max_results)
		elif file_name.ends_with(".tscn") or file_name.ends_with(".tres"):
			# Read file content and search for the resource path
			var file := FileAccess.open(full_path, FileAccess.READ)
			if file:
				var content := file.get_as_text()
				file.close()

				# Check if the target resource is referenced
				# Resources are referenced like: [ext_resource type="..." path="res://..." id="..."]
				# Or: load("res://...")
				if target_resource in content:
					references.append({
						"path": full_path,
						"name": file_name,
						"type": "scene" if file_name.ends_with(".tscn") else "resource",
					})

		file_name = dir.get_next()
	dir.list_dir_end()


# -- CAMERA HANDLERS --

func _handle_camera_save(params: Dictionary) -> Dictionary:
	## Save the current editor camera position as a named bookmark.
	var bookmark_name: String = params.get("name", "")

	if bookmark_name.is_empty():
		return _error_response("EARG", "name is required", "")

	# Get the 2D editor viewport camera
	var viewport := EditorInterface.get_editor_viewport_2d()
	if viewport:
		var transform := viewport.global_canvas_transform
		_camera_bookmarks[bookmark_name] = {
			"type": "2d",
			"offset_x": -transform.origin.x / transform.get_scale().x,
			"offset_y": -transform.origin.y / transform.get_scale().y,
			"zoom": transform.get_scale().x,
		}
		return _success_response({
			"ok": true,
			"name": bookmark_name,
			"type": "2d",
			"bookmark": _camera_bookmarks[bookmark_name],
		})

	# Try 3D viewport
	var viewport_3d := EditorInterface.get_editor_viewport_3d()
	if viewport_3d:
		var camera := viewport_3d.get_camera_3d()
		if camera:
			_camera_bookmarks[bookmark_name] = {
				"type": "3d",
				"position_x": camera.global_position.x,
				"position_y": camera.global_position.y,
				"position_z": camera.global_position.z,
				"rotation_x": camera.rotation.x,
				"rotation_y": camera.rotation.y,
				"rotation_z": camera.rotation.z,
			}
			return _success_response({
				"ok": true,
				"name": bookmark_name,
				"type": "3d",
				"bookmark": _camera_bookmarks[bookmark_name],
			})

	return _error_response("EEDITOR_STATE", "Could not access editor camera", "")


func _handle_camera_restore(params: Dictionary) -> Dictionary:
	## Restore a saved camera bookmark.
	var bookmark_name: String = params.get("name", "")

	if bookmark_name.is_empty():
		return _error_response("EARG", "name is required", "")

	if not _camera_bookmarks.has(bookmark_name):
		var available := ", ".join(_camera_bookmarks.keys())
		return _error_response("EBOOKMARK_NOT_FOUND",
			"Bookmark not found: " + bookmark_name,
			"Available bookmarks: " + (available if available else "(none)"))

	var bookmark: Dictionary = _camera_bookmarks[bookmark_name]

	if bookmark.get("type") == "2d":
		var viewport := EditorInterface.get_editor_viewport_2d()
		if viewport:
			var zoom: float = bookmark.get("zoom", 1.0)
			var offset_x: float = bookmark.get("offset_x", 0.0)
			var offset_y: float = bookmark.get("offset_y", 0.0)

			# Set the canvas transform
			var transform := Transform2D()
			transform = transform.scaled(Vector2(zoom, zoom))
			transform.origin = Vector2(-offset_x * zoom, -offset_y * zoom)
			viewport.global_canvas_transform = transform

			return _success_response({
				"ok": true,
				"name": bookmark_name,
				"restored": bookmark,
			})

	# 3D camera restoration is more complex and may not be directly supported
	# via EditorInterface in Godot 4
	if bookmark.get("type") == "3d":
		return _error_response("EUNSUPPORTED",
			"3D camera restoration not yet implemented",
			"Use the 2D viewport for now.")

	return _error_response("EEDITOR_STATE", "Could not restore camera position", "")


func _handle_camera_list() -> Dictionary:
	## List all saved camera bookmarks.
	var bookmarks: Array[Dictionary] = []
	for name in _camera_bookmarks:
		var bookmark: Dictionary = _camera_bookmarks[name].duplicate()
		bookmark["name"] = name
		bookmarks.append(bookmark)

	return _success_response({
		"bookmarks": bookmarks,
		"count": bookmarks.size(),
	})


# -- HELPER FUNCTIONS --

func _get_scene_path(node: Node, scene_root: Node) -> String:
	## Get a clean path relative to the scene root, prefixed with the root name.
	if node == scene_root:
		return "/root/" + node.name

	var path_from_root := scene_root.get_path_to(node)
	return "/root/" + scene_root.name + "/" + str(path_from_root)


func _resolve_node_path(path: String, scene_root: Node) -> Node:
	## Resolve a path like /root/GameRoot/Player to the actual node.
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
			var relative_path := "/".join(parts.slice(1))
			return scene_root.get_node_or_null(relative_path)
		else:
			return scene_root.get_node_or_null(rest)

	return scene_root.get_node_or_null(path)


# -- RESPONSE HELPERS --

func _success_response(data: Dictionary) -> Dictionary:
	return {
		"status": "success",
		"data": data,
		"error": null,
	}


func _error_response(code: String, message: String, hint: String) -> Dictionary:
	return {
		"status": "error",
		"data": null,
		"error": {
			"code": code,
			"message": message,
			"hint": hint,
		},
	}
