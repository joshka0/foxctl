## Physics handler for GodotAIBridge - handles physics layer operations.
extends RefCounted
class_name PhysicsHandler


func _init() -> void:
	pass


func handle(action: String, params: Dictionary) -> Dictionary:
	match action:
		"physics_layer_get":
			return _handle_physics_layer_get(params)
		"physics_layer_set":
			return _handle_physics_layer_set(params)
		_:
			return _error_response("EARG", "PhysicsHandler: Unknown action: " + action, "")


func _handle_physics_layer_get(params: Dictionary) -> Dictionary:
	## Get physics layer information.
	var layer: int = params.get("layer", -1)
	var dimension: String = params.get("dimension", "2d")  # 2d or 3d

	# Validate dimension
	if dimension != "2d" and dimension != "3d":
		return _error_response("EARG", "dimension must be '2d' or '3d'", "")

	var prefix := "layer_names/" + dimension + "_physics/"

	if layer >= 0:
		# Get specific layer
		if layer > 31:
			return _error_response("EARG", "layer must be between 0 and 31", "")

		var layer_key := prefix + "layer_" + str(layer + 1)
		var layer_name: String = ProjectSettings.get_setting(layer_key, "")

		return _success_response({
			"layer": layer,
			"dimension": dimension,
			"name": layer_name,
		})
	else:
		# Get all layers
		var layers := []
		for i in range(32):
			var layer_key := prefix + "layer_" + str(i + 1)
			var layer_name: String = ProjectSettings.get_setting(layer_key, "")
			if not layer_name.is_empty():
				layers.append({
					"layer": i,
					"name": layer_name,
				})

		return _success_response({
			"dimension": dimension,
			"layers": layers,
			"total_configured": layers.size(),
		})


func _handle_physics_layer_set(params: Dictionary) -> Dictionary:
	## Set physics layer name.
	var layer: int = params.get("layer", -1)
	var name: String = params.get("name", "")
	var dimension: String = params.get("dimension", "2d")  # 2d or 3d

	if layer < 0 or layer > 31:
		return _error_response("EARG", "layer must be between 0 and 31", "")

	# Validate dimension
	if dimension != "2d" and dimension != "3d":
		return _error_response("EARG", "dimension must be '2d' or '3d'", "")

	var layer_key := "layer_names/" + dimension + "_physics/layer_" + str(layer + 1)
	var old_name: String = ProjectSettings.get_setting(layer_key, "")

	ProjectSettings.set_setting(layer_key, name)

	# Save project settings
	var err := ProjectSettings.save()
	if err != OK:
		return _error_response("EIO", "Failed to save project settings", "")

	return _success_response({
		"ok": true,
		"layer": layer,
		"dimension": dimension,
		"old_name": old_name,
		"new_name": name,
	})


func _success_response(data: Dictionary) -> Dictionary:
	return {"status": "success", "data": data, "error": null}


func _error_response(code: String, message: String, hint: String) -> Dictionary:
	return {"status": "error", "data": null, "error": {"code": code, "message": message, "hint": hint}}
