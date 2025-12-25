## Theme handler for GodotAIBridge - handles theme property operations.
extends RefCounted
class_name ThemeHandler


func _init() -> void:
	pass


func handle(action: String, params: Dictionary) -> Dictionary:
	match action:
		"theme_get":
			return _handle_theme_get(params)
		"theme_set":
			return _handle_theme_set(params)
		_:
			return _error_response("EACTION", "ThemeHandler: Unknown action: " + action, "")


func _handle_theme_get(params: Dictionary) -> Dictionary:
	## Get a theme property value.
	var theme_path: String = params.get("theme_path", "")
	var theme_type: String = params.get("type", "")  # color, constant, font, icon, stylebox
	var theme_item: String = params.get("item", "")
	var node_type: String = params.get("node_type", "")  # e.g., "Button", "Label"

	if theme_type.is_empty():
		return _error_response("EARG", "type is required (color, constant, font, icon, stylebox)", "")
	if theme_item.is_empty():
		return _error_response("EARG", "item is required", "")
	if node_type.is_empty():
		return _error_response("EARG", "node_type is required (e.g., Button, Label)", "")

	var theme: Theme = null

	if not theme_path.is_empty():
		if not theme_path.begins_with("res://"):
			theme_path = "res://" + theme_path.lstrip("/")
		if not ResourceLoader.exists(theme_path):
			return _error_response("ERESOURCE_NOT_FOUND", "Theme not found: " + theme_path, "")
		theme = load(theme_path)
		if not theme or not theme is Theme:
			return _error_response("ERESOURCE_INVALID", "Not a valid theme: " + theme_path, "")
	else:
		# Use the default project theme
		theme = ThemeDB.get_project_theme()
		if not theme:
			return _error_response("ETHEME_NOT_FOUND", "No project theme found", "")

	var value: Variant = null
	var has_value := false

	match theme_type.to_lower():
		"color":
			if theme.has_color(theme_item, node_type):
				value = theme.get_color(theme_item, node_type)
				has_value = true
		"constant":
			if theme.has_constant(theme_item, node_type):
				value = theme.get_constant(theme_item, node_type)
				has_value = true
		"font":
			if theme.has_font(theme_item, node_type):
				var font := theme.get_font(theme_item, node_type)
				value = font.resource_path if font else ""
				has_value = true
		"icon":
			if theme.has_icon(theme_item, node_type):
				var icon := theme.get_icon(theme_item, node_type)
				value = icon.resource_path if icon else ""
				has_value = true
		"stylebox":
			if theme.has_stylebox(theme_item, node_type):
				var stylebox := theme.get_stylebox(theme_item, node_type)
				value = stylebox.get_class() if stylebox else ""
				has_value = true
		_:
			return _error_response("EARG", "Invalid type: " + theme_type,
				"Valid types: color, constant, font, icon, stylebox")

	if not has_value:
		return _error_response("ETHEME_ITEM_NOT_FOUND",
			"Theme item not found: %s/%s/%s" % [node_type, theme_type, theme_item], "")

	return _success_response({
		"type": theme_type,
		"item": theme_item,
		"node_type": node_type,
		"value": _value_to_string(value),
	})


func _handle_theme_set(params: Dictionary) -> Dictionary:
	## Set a theme property value.
	var theme_path: String = params.get("theme_path", "")
	var theme_type: String = params.get("type", "")
	var theme_item: String = params.get("item", "")
	var node_type: String = params.get("node_type", "")
	var value_str: String = params.get("value", "")
	var dry_run: bool = params.get("dry_run", false)

	if theme_type.is_empty():
		return _error_response("EARG", "type is required", "")
	if theme_item.is_empty():
		return _error_response("EARG", "item is required", "")
	if node_type.is_empty():
		return _error_response("EARG", "node_type is required", "")

	var theme: Theme = null

	if not theme_path.is_empty():
		if not theme_path.begins_with("res://"):
			theme_path = "res://" + theme_path.lstrip("/")
		if not ResourceLoader.exists(theme_path):
			return _error_response("ERESOURCE_NOT_FOUND", "Theme not found: " + theme_path, "")
		theme = load(theme_path)
		if not theme or not theme is Theme:
			return _error_response("ERESOURCE_INVALID", "Not a valid theme: " + theme_path, "")
	else:
		theme = ThemeDB.get_project_theme()
		if not theme:
			return _error_response("ETHEME_NOT_FOUND", "No project theme found", "")

	if dry_run:
		return _success_response({
			"dry_run": true,
			"type": theme_type,
			"item": theme_item,
			"node_type": node_type,
			"new_value": value_str,
		})

	match theme_type.to_lower():
		"color":
			var color := _parse_color(value_str)
			if color == null:
				return _error_response("ETYPE_CONVERSION", "Invalid color: " + value_str,
					"Use format: Color(r, g, b), Color(r, g, b, a), or #rrggbb")
			theme.set_color(theme_item, node_type, color)
		"constant":
			if not value_str.is_valid_int():
				return _error_response("ETYPE_CONVERSION", "Invalid constant (must be int): " + value_str, "")
			theme.set_constant(theme_item, node_type, value_str.to_int())
		_:
			return _error_response("EUNSUPPORTED",
				"Setting %s type not yet implemented" % theme_type,
				"Currently only color and constant are supported for setting.")

	# Save theme
	var save_path := theme_path
	if save_path.is_empty():
		# Save to project theme location if no path specified
		save_path = theme.resource_path
		if save_path.is_empty():
			# Create a default project theme path
			save_path = "res://theme.tres"

	var err := ResourceSaver.save(theme, save_path)
	if err != OK:
		return _error_response("EIO", "Failed to save theme: error %d" % err, "")

	return _success_response({
		"ok": true,
		"type": theme_type,
		"item": theme_item,
		"node_type": node_type,
		"new_value": value_str,
		"saved_to": save_path,
	})


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
