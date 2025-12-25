## Plugins handler for GodotAIBridge - handles editor plugin operations.
extends RefCounted
class_name PluginsHandler


func _init() -> void:
	pass


func handle(action: String, params: Dictionary) -> Dictionary:
	match action:
		"plugin_list":
			return _handle_plugin_list(params)
		"plugin_enable":
			return _handle_plugin_enable(params)
		"plugin_disable":
			return _handle_plugin_disable(params)
		_:
			return _error_response("EARG", "PluginsHandler: Unknown action: " + action, "")


func _handle_plugin_list(_params: Dictionary) -> Dictionary:
	## List all available editor plugins.
	var plugins: Array[Dictionary] = []

	# Scan addons directory
	var addons_dir := DirAccess.open("res://addons")
	if addons_dir:
		addons_dir.list_dir_begin()
		var dir_name := addons_dir.get_next()
		while dir_name != "":
			if addons_dir.current_is_dir() and not dir_name.begins_with("."):
				var plugin_cfg_path := "res://addons/" + dir_name + "/plugin.cfg"
				if FileAccess.file_exists(plugin_cfg_path):
					var plugin_info := _parse_plugin_cfg(plugin_cfg_path)
					if not plugin_info.is_empty():
						plugin_info["directory"] = dir_name
						plugin_info["enabled"] = EditorInterface.is_plugin_enabled(dir_name)
						plugins.append(plugin_info)
			dir_name = addons_dir.get_next()
		addons_dir.list_dir_end()

	return _success_response({
		"plugins": plugins,
		"count": plugins.size(),
	})


func _handle_plugin_enable(params: Dictionary) -> Dictionary:
	## Enable an editor plugin.
	var plugin_name: String = params.get("name", "")

	if plugin_name.is_empty():
		return _error_response("EARG", "name is required", "")

	# Check plugin exists
	var plugin_cfg_path := "res://addons/" + plugin_name + "/plugin.cfg"
	if not FileAccess.file_exists(plugin_cfg_path):
		return _error_response("ENOTFOUND", "Plugin not found: " + plugin_name,
			"Ensure the plugin is installed in res://addons/" + plugin_name + "/")

	var was_enabled := EditorInterface.is_plugin_enabled(plugin_name)

	if was_enabled:
		return _success_response({
			"ok": true,
			"name": plugin_name,
			"already_enabled": true,
		})

	EditorInterface.set_plugin_enabled(plugin_name, true)

	return _success_response({
		"ok": true,
		"name": plugin_name,
		"enabled": true,
	})


func _handle_plugin_disable(params: Dictionary) -> Dictionary:
	## Disable an editor plugin.
	var plugin_name: String = params.get("name", "")

	if plugin_name.is_empty():
		return _error_response("EARG", "name is required", "")

	# Check plugin exists
	var plugin_cfg_path := "res://addons/" + plugin_name + "/plugin.cfg"
	if not FileAccess.file_exists(plugin_cfg_path):
		return _error_response("ENOTFOUND", "Plugin not found: " + plugin_name,
			"Ensure the plugin is installed in res://addons/" + plugin_name + "/")

	var was_enabled := EditorInterface.is_plugin_enabled(plugin_name)

	if not was_enabled:
		return _success_response({
			"ok": true,
			"name": plugin_name,
			"already_disabled": true,
		})

	EditorInterface.set_plugin_enabled(plugin_name, false)

	return _success_response({
		"ok": true,
		"name": plugin_name,
		"disabled": true,
	})


func _parse_plugin_cfg(path: String) -> Dictionary:
	## Parse a plugin.cfg file.
	var result: Dictionary = {}

	var file := FileAccess.open(path, FileAccess.READ)
	if not file:
		return result

	var content := file.get_as_text()
	file.close()

	var in_plugin_section := false
	for line in content.split("\n"):
		line = line.strip_edges()

		if line == "[plugin]":
			in_plugin_section = true
			continue
		elif line.begins_with("[") and line != "[plugin]":
			in_plugin_section = false
			continue

		if in_plugin_section and "=" in line:
			var parts := line.split("=", true, 1)
			if parts.size() == 2:
				var key := parts[0].strip_edges()
				var value := parts[1].strip_edges().trim_prefix("\"").trim_suffix("\"")
				result[key] = value

	return result


func _success_response(data: Dictionary) -> Dictionary:
	return {"status": "success", "data": data, "error": null}


func _error_response(code: String, message: String, hint: String) -> Dictionary:
	return {"status": "error", "data": null, "error": {"code": code, "message": message, "hint": hint}}
