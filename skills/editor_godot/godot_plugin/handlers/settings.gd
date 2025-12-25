## Settings handler for GodotAIBridge - handles project settings operations.
extends RefCounted
class_name SettingsHandler


func _init() -> void:
	pass


func handle(action: String, params: Dictionary) -> Dictionary:
	match action:
		"project_setting_get":
			return _handle_project_setting_get(params)
		"project_setting_set":
			return _handle_project_setting_set(params)
		_:
			return _error_response("EACTION", "SettingsHandler: Unknown action: " + action, "")


func _handle_project_setting_get(params: Dictionary) -> Dictionary:
	## Get a project setting value.
	var key: String = params.get("key", "")

	if key.is_empty():
		return _error_response("EARG", "key is required", "")

	if not ProjectSettings.has_setting(key):
		return _error_response("ESETTING_NOT_FOUND", "Setting not found: " + key,
			"Use a valid project setting path like 'application/config/name'")

	var value = ProjectSettings.get_setting(key)
	var value_str := _value_to_string(value)

	return _success_response({
		"key": key,
		"value": value_str,
		"type": type_string(typeof(value)),
	})


func _handle_project_setting_set(params: Dictionary) -> Dictionary:
	## Set a project setting value.
	var key: String = params.get("key", "")
	var value_str: String = params.get("value", "")
	var dry_run: bool = params.get("dry_run", false)

	if key.is_empty():
		return _error_response("EARG", "key is required", "")

	# Get current value if exists
	var old_value = null
	var existed := ProjectSettings.has_setting(key)
	if existed:
		old_value = ProjectSettings.get_setting(key)

	# Parse new value - try to infer type from old value or string
	var new_value: Variant
	if existed and old_value != null:
		new_value = _convert_value(value_str, typeof(old_value))
	else:
		new_value = _parse_value(value_str)

	if dry_run:
		return _success_response({
			"dry_run": true,
			"key": key,
			"old_value": _value_to_string(old_value) if old_value != null else null,
			"new_value": value_str,
			"existed": existed,
		})

	# Set the setting
	ProjectSettings.set_setting(key, new_value)

	# Save project settings
	var err := ProjectSettings.save()
	if err != OK:
		return _error_response("EIO", "Failed to save project settings: error %d" % err, "")

	return _success_response({
		"ok": true,
		"key": key,
		"old_value": _value_to_string(old_value) if old_value != null else null,
		"new_value": _value_to_string(new_value),
		"created": not existed,
	})


func _parse_value(value_str: String) -> Variant:
	## Try to parse a string into the appropriate type.
	# Try boolean
	if value_str.to_lower() == "true":
		return true
	if value_str.to_lower() == "false":
		return false

	# Try integer
	if value_str.is_valid_int():
		return value_str.to_int()

	# Try float
	if value_str.is_valid_float():
		return value_str.to_float()

	# Try Vector2
	if value_str.begins_with("Vector2"):
		var parsed = _parse_vector2(value_str)
		if parsed != null:
			return parsed

	# Try Color
	if value_str.begins_with("Color") or value_str.begins_with("#"):
		var parsed = _parse_color(value_str)
		if parsed != null:
			return parsed

	# Default to string
	return value_str


func _convert_value(value_str: String, target_type: int) -> Variant:
	if value_str.is_empty():
		return null

	match target_type:
		TYPE_INT:
			if value_str.is_valid_int():
				return value_str.to_int()
			return null
		TYPE_FLOAT:
			if value_str.is_valid_float():
				return value_str.to_float()
			return null
		TYPE_BOOL:
			var lower := value_str.to_lower()
			if lower == "true":
				return true
			if lower == "false":
				return false
			return null
		TYPE_STRING:
			return value_str
		TYPE_VECTOR2:
			return _parse_vector2(value_str)
		TYPE_COLOR:
			return _parse_color(value_str)

	return _parse_value(value_str)


func _parse_vector2(s: String) -> Variant:
	var cleaned := s.replace("Vector2", "").replace("(", "").replace(")", "").strip_edges()
	var parts := cleaned.split(",")
	if parts.size() == 2:
		var x := parts[0].strip_edges()
		var y := parts[1].strip_edges()
		if x.is_valid_float() and y.is_valid_float():
			return Vector2(x.to_float(), y.to_float())
	return null


func _parse_color(s: String) -> Variant:
	if s.begins_with("#"):
		return Color.html(s)
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
	return null


func _value_to_string(value: Variant) -> String:
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


func _success_response(data: Dictionary) -> Dictionary:
	return {"status": "success", "data": data, "error": null}


func _error_response(code: String, message: String, hint: String) -> Dictionary:
	return {"status": "error", "data": null, "error": {"code": code, "message": message, "hint": hint}}
