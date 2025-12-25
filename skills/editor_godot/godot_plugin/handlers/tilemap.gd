## TileMap handler for GodotAIBridge - handles TileMap operations.
extends RefCounted
class_name TileMapHandler


func _init() -> void:
	pass


func handle(action: String, params: Dictionary) -> Dictionary:
	match action:
		"tilemap_get_cell":
			return _handle_tilemap_get_cell(params)
		"tilemap_set_cell":
			return _handle_tilemap_set_cell(params)
		_:
			return _error_response("EACTION", "TileMapHandler: Unknown action: " + action, "")


func _handle_tilemap_get_cell(params: Dictionary) -> Dictionary:
	## Get tile at position in a TileMap.
	var node_path: String = params.get("node_path", "")
	var x: int = params.get("x", 0)
	var y: int = params.get("y", 0)
	var layer: int = params.get("layer", 0)

	if node_path.is_empty():
		return _error_response("EARG", "node_path is required", "")

	var node := _resolve_node_path(node_path)
	if not node:
		return _error_response("ENODE", "Node not found: " + node_path, "")

	if not node is TileMapLayer and not node is TileMap:
		return _error_response("ETILEMAP", "Node is not a TileMap or TileMapLayer: " + node_path,
			"Provide a path to a TileMap or TileMapLayer node.")

	var coords := Vector2i(x, y)
	var source_id: int
	var atlas_coords: Vector2i
	var alternative_tile: int

	if node is TileMapLayer:
		var tilemap_layer := node as TileMapLayer
		source_id = tilemap_layer.get_cell_source_id(coords)
		atlas_coords = tilemap_layer.get_cell_atlas_coords(coords)
		alternative_tile = tilemap_layer.get_cell_alternative_tile(coords)
	else:
		# TileMap (deprecated but still supported)
		var tilemap := node as TileMap
		source_id = tilemap.get_cell_source_id(layer, coords)
		atlas_coords = tilemap.get_cell_atlas_coords(layer, coords)
		alternative_tile = tilemap.get_cell_alternative_tile(layer, coords)

	var is_empty := source_id == -1

	return _success_response({
		"node_path": node_path,
		"x": x,
		"y": y,
		"layer": layer,
		"is_empty": is_empty,
		"source_id": source_id,
		"atlas_coords": {"x": atlas_coords.x, "y": atlas_coords.y},
		"alternative_tile": alternative_tile,
	})


func _handle_tilemap_set_cell(params: Dictionary) -> Dictionary:
	## Set tile at position in a TileMap.
	var node_path: String = params.get("node_path", "")
	var x: int = params.get("x", 0)
	var y: int = params.get("y", 0)
	var layer: int = params.get("layer", 0)
	var source_id: int = params.get("source_id", -1)
	var atlas_x: int = params.get("atlas_x", 0)
	var atlas_y: int = params.get("atlas_y", 0)
	var alternative_tile: int = params.get("alternative_tile", 0)
	var erase: bool = params.get("erase", false)

	if node_path.is_empty():
		return _error_response("EARG", "node_path is required", "")

	var node := _resolve_node_path(node_path)
	if not node:
		return _error_response("ENODE", "Node not found: " + node_path, "")

	if not node is TileMapLayer and not node is TileMap:
		return _error_response("ETILEMAP", "Node is not a TileMap or TileMapLayer: " + node_path,
			"Provide a path to a TileMap or TileMapLayer node.")

	var coords := Vector2i(x, y)
	var atlas_coords := Vector2i(atlas_x, atlas_y)

	if node is TileMapLayer:
		var tilemap_layer := node as TileMapLayer
		if erase:
			tilemap_layer.erase_cell(coords)
		else:
			tilemap_layer.set_cell(coords, source_id, atlas_coords, alternative_tile)
	else:
		# TileMap (deprecated but still supported)
		var tilemap := node as TileMap
		if erase:
			tilemap.erase_cell(layer, coords)
		else:
			tilemap.set_cell(layer, coords, source_id, atlas_coords, alternative_tile)

	return _success_response({
		"ok": true,
		"node_path": node_path,
		"x": x,
		"y": y,
		"layer": layer,
		"erased": erase,
		"source_id": source_id if not erase else -1,
		"atlas_coords": {"x": atlas_x, "y": atlas_y} if not erase else null,
	})


func _resolve_node_path(path: String) -> Node:
	## Resolve a node path to a Node object.
	if path.is_empty():
		return null

	# Get the edited scene root
	var edited_scene := EditorInterface.get_edited_scene_root()
	if not edited_scene:
		return null

	# Handle absolute paths starting with /root/
	if path.begins_with("/root/"):
		var parts := path.split("/")
		if parts.size() < 3:
			return edited_scene
		# Skip /root/SceneName and get the rest
		var relative_path := "/".join(parts.slice(3))
		if relative_path.is_empty():
			return edited_scene
		return edited_scene.get_node_or_null(relative_path)

	# Handle relative paths
	return edited_scene.get_node_or_null(path)


func _success_response(data: Dictionary) -> Dictionary:
	return {"status": "success", "data": data, "error": null}


func _error_response(code: String, message: String, hint: String) -> Dictionary:
	return {"status": "error", "data": null, "error": {"code": code, "message": message, "hint": hint}}
