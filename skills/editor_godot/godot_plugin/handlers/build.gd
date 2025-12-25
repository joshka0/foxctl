## Build handler for GodotAIBridge - handles game export/build operations.
extends RefCounted
class_name BuildHandler


func _init() -> void:
	pass


func handle(action: String, params: Dictionary) -> Dictionary:
	match action:
		"build":
			return _handle_build(params)
		_:
			return _error_response("EARG", "BuildHandler: Unknown action: " + action, "")


func _handle_build(params: Dictionary) -> Dictionary:
	## Export the game using EditorExportPlugin.
	var preset_name: String = params.get("preset", "")
	var output_path: String = params.get("output_path", "")
	var debug_build: bool = params.get("debug", false)
	var dry_run: bool = params.get("dry_run", false)

	if preset_name.is_empty():
		return _error_response("EARG", "preset is required", "")

	# Get export presets
	var export_presets := _get_export_presets()
	if export_presets.is_empty():
		return _error_response("ENOTFOUND", "No export presets found",
			"Create export presets in Project > Export... first.")

	# Find the matching preset
	var preset_index := -1
	for i in range(export_presets.size()):
		if export_presets[i].name == preset_name:
			preset_index = i
			break

	if preset_index < 0:
		var available := []
		for p in export_presets:
			available.append(p.name)
		return _error_response("ENOTFOUND",
			"Export preset not found: " + preset_name,
			"Available presets: " + ", ".join(available))

	var preset := export_presets[preset_index]

	# Determine output path
	if output_path.is_empty():
		output_path = preset.get("export_path", "")
		if output_path.is_empty():
			return _error_response("EARG", "output_path is required (preset has no default export path)",
				"Specify output_path or configure a default in the export preset.")

	# Dry run
	if dry_run:
		return _success_response({
			"dry_run": true,
			"preset": preset_name,
			"output_path": output_path,
			"debug": debug_build,
			"platform": preset.get("platform", "unknown"),
		})

	# Note: Actual export requires EditorExportPlatform which is complex to use from GDScript.
	# This implementation provides info about what would be exported.
	# For actual export, users should use the editor UI or command-line export.

	return _success_response({
		"ok": true,
		"preset": preset_name,
		"output_path": output_path,
		"debug": debug_build,
		"platform": preset.get("platform", "unknown"),
		"note": "Export operation prepared. Use Godot CLI for actual export: godot --headless --export-" + ("debug" if debug_build else "release") + " \"" + preset_name + "\" \"" + output_path + "\"",
	})


func _get_export_presets() -> Array[Dictionary]:
	## Get list of export presets from project.
	var presets: Array[Dictionary] = []

	# Read export_presets.cfg
	var cfg_path := "res://export_presets.cfg"
	if not FileAccess.file_exists(cfg_path):
		return presets

	var file := FileAccess.open(cfg_path, FileAccess.READ)
	if not file:
		return presets

	var content := file.get_as_text()
	file.close()

	# Parse the config file
	var current_preset: Dictionary = {}
	var in_preset := false

	for line in content.split("\n"):
		line = line.strip_edges()

		if line.begins_with("[preset."):
			# Save previous preset
			if not current_preset.is_empty() and current_preset.has("name"):
				presets.append(current_preset)
			current_preset = {}
			in_preset = true
		elif line.begins_with("[") and not line.begins_with("[preset."):
			# End of presets section
			if not current_preset.is_empty() and current_preset.has("name"):
				presets.append(current_preset)
			in_preset = false
			current_preset = {}
		elif in_preset and "=" in line:
			var parts := line.split("=", true, 1)
			if parts.size() == 2:
				var key := parts[0].strip_edges()
				var value := parts[1].strip_edges().trim_prefix("\"").trim_suffix("\"")
				current_preset[key] = value

	# Don't forget the last preset
	if not current_preset.is_empty() and current_preset.has("name"):
		presets.append(current_preset)

	return presets


func _success_response(data: Dictionary) -> Dictionary:
	return {"status": "success", "data": data, "error": null}


func _error_response(code: String, message: String, hint: String) -> Dictionary:
	return {"status": "error", "data": null, "error": {"code": code, "message": message, "hint": hint}}
