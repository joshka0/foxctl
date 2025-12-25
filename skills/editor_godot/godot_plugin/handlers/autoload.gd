## Autoload handler for GodotAIBridge - handles autoload/singleton operations.
extends RefCounted
class_name AutoloadHandler


func _init() -> void:
	pass


func handle(action: String, params: Dictionary) -> Dictionary:
	match action:
		"autoload_list":
			return _handle_autoload_list(params)
		"autoload_add":
			return _handle_autoload_add(params)
		"autoload_remove":
			return _handle_autoload_remove(params)
		_:
			return _error_response("EACTION", "AutoloadHandler: Unknown action: " + action, "")


func _handle_autoload_list(_params: Dictionary) -> Dictionary:
	## List all autoload singletons.
	var autoloads: Array[Dictionary] = []

	# Get autoloads from project settings
	for setting in ProjectSettings.get_property_list():
		var name: String = setting.name
		if name.begins_with("autoload/"):
			var autoload_name := name.substr(9)  # Remove "autoload/"
			var value = ProjectSettings.get_setting(name)

			# Parse the value - format is "*res://path.gd" or "res://path.gd"
			# * prefix means it's enabled as singleton
			var is_singleton := false
			var script_path: String = str(value)
			if script_path.begins_with("*"):
				is_singleton = true
				script_path = script_path.substr(1)

			autoloads.append({
				"name": autoload_name,
				"path": script_path,
				"singleton": is_singleton,
			})

	return _success_response({
		"autoloads": autoloads,
		"count": autoloads.size(),
	})


func _handle_autoload_add(params: Dictionary) -> Dictionary:
	## Add an autoload singleton.
	var autoload_name: String = params.get("name", "")
	var autoload_path: String = params.get("path", "")

	if autoload_name.is_empty():
		return _error_response("EARG", "name is required", "")
	if autoload_path.is_empty():
		return _error_response("EARG", "path is required", "")

	if not autoload_path.begins_with("res://"):
		autoload_path = "res://" + autoload_path.lstrip("/")

	# Validate the script exists
	if not FileAccess.file_exists(autoload_path):
		return _error_response("ESCRIPT_NOT_FOUND", "Script not found: " + autoload_path, "")

	# Check if autoload already exists
	var setting_key := "autoload/" + autoload_name
	var existed := ProjectSettings.has_setting(setting_key)

	# Set the autoload (with * prefix for singleton)
	ProjectSettings.set_setting(setting_key, "*" + autoload_path)

	# Make sure it's added to the project order
	ProjectSettings.set_initial_value(setting_key, "*" + autoload_path)

	# Save project settings
	var err := ProjectSettings.save()
	if err != OK:
		return _error_response("EIO", "Failed to save project settings: error %d" % err, "")

	return _success_response({
		"ok": true,
		"name": autoload_name,
		"path": autoload_path,
		"replaced": existed,
		"note": "Restart the editor for changes to take effect.",
	})


func _handle_autoload_remove(params: Dictionary) -> Dictionary:
	## Remove an autoload singleton.
	var autoload_name: String = params.get("name", "")

	if autoload_name.is_empty():
		return _error_response("EARG", "name is required", "")

	var setting_key := "autoload/" + autoload_name

	if not ProjectSettings.has_setting(setting_key):
		return _error_response("EAUTOLOAD_NOT_FOUND", "Autoload not found: " + autoload_name, "")

	# Get the path before removing (for response)
	var old_value = ProjectSettings.get_setting(setting_key)
	var old_path: String = str(old_value)
	if old_path.begins_with("*"):
		old_path = old_path.substr(1)

	# Remove the setting
	ProjectSettings.set_setting(setting_key, null)

	# Save project settings
	var err := ProjectSettings.save()
	if err != OK:
		return _error_response("EIO", "Failed to save project settings: error %d" % err, "")

	return _success_response({
		"ok": true,
		"name": autoload_name,
		"removed_path": old_path,
		"note": "Restart the editor for changes to take effect.",
	})


func _success_response(data: Dictionary) -> Dictionary:
	return {"status": "success", "data": data, "error": null}


func _error_response(code: String, message: String, hint: String) -> Dictionary:
	return {"status": "error", "data": null, "error": {"code": code, "message": message, "hint": hint}}
