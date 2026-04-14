## Scripts handler for GodotAIBridge - handles GDScript operations.
extends RefCounted
class_name ScriptsHandler


func _init() -> void:
	pass


func handle(action: String, params: Dictionary) -> Dictionary:
	match action:
		"script_create":
			return _handle_script_create(params)
		"script_read":
			return _handle_script_read(params)
		"script_edit":
			return _handle_script_edit(params)
		_:
			return _error_response("EACTION", "ScriptsHandler: Unknown action: " + action, "")


func _handle_script_create(params: Dictionary) -> Dictionary:
	## Create a new GDScript file with a safe template.
	var script_path: String = params.get("path", "")
	var extends_class: String = params.get("extends", "Node")
	var exports: Array = params.get("exports", [])
	var methods: Array = params.get("methods", [])
	var signals_list: Array = params.get("signals", [])
	var overwrite: bool = params.get("overwrite", false)

	if script_path.is_empty():
		return _error_response("EARG", "path is required", "")

	if not script_path.begins_with("res://"):
		script_path = "res://" + script_path.lstrip("/")

	if not script_path.ends_with(".gd"):
		script_path += ".gd"

	# Check if file already exists
	if FileAccess.file_exists(script_path) and not overwrite:
		return _error_response("ESCRIPT_EXISTS", "Script already exists: " + script_path,
			"Use overwrite=true to replace the existing script.")

	# Validate extends class
	if not ClassDB.class_exists(extends_class):
		return _error_response("ETYPE_INVALID", "Invalid base class: " + extends_class,
			"Use a valid Godot class name like Node, Node2D, CharacterBody2D, etc.")

	# Build script content
	var content := "extends %s\n" % extends_class
	content += "## Auto-generated script by foxctl\n\n"

	# Add signals
	for sig in signals_list:
		var sig_name: String = sig if sig is String else sig.get("name", "")
		if not sig_name.is_empty():
			content += "signal %s\n" % sig_name

	if not signals_list.is_empty():
		content += "\n"

	# Add exports
	for exp in exports:
		var exp_name: String = ""
		var exp_type: String = "Variant"
		var exp_default: String = ""

		if exp is String:
			exp_name = exp
		elif exp is Dictionary:
			exp_name = exp.get("name", "")
			exp_type = exp.get("type", "Variant")
			exp_default = exp.get("default", "")

		if not exp_name.is_empty():
			if exp_default.is_empty():
				content += "@export var %s: %s\n" % [exp_name, exp_type]
			else:
				content += "@export var %s: %s = %s\n" % [exp_name, exp_type, exp_default]

	if not exports.is_empty():
		content += "\n"

	# Add _ready if no methods specified
	if methods.is_empty():
		content += "\nfunc _ready() -> void:\n\tpass\n"
	else:
		for method in methods:
			var method_name: String = ""
			var method_args: String = ""
			var method_return: String = "void"

			if method is String:
				method_name = method
			elif method is Dictionary:
				method_name = method.get("name", "")
				method_args = method.get("args", "")
				method_return = method.get("return", "void")

			# Validate method_name to prevent GDScript injection
			if not method_name.is_empty() and not _is_valid_identifier(method_name):
				return _error_response("EARG", "Invalid method name: " + method_name,
					"Method names must be valid GDScript identifiers (letters, digits, underscores)")

			# Validate method_args to prevent GDScript injection
			if not method_args.is_empty() and not _is_valid_args(method_args):
				return _error_response("EARG", "Invalid method args: " + method_args,
					"Method args must contain only valid parameter declarations")

			if not method_name.is_empty():
				content += "\nfunc %s(%s) -> %s:\n\tpass\n" % [method_name, method_args, method_return]

	# Create directory if needed
	var dir_path := script_path.get_base_dir()
	if not DirAccess.dir_exists_absolute(dir_path):
		var err := DirAccess.make_dir_recursive_absolute(dir_path)
		if err != OK:
			return _error_response("EIO", "Failed to create directory: " + dir_path, "")

	# Write the file
	var file := FileAccess.open(script_path, FileAccess.WRITE)
	if not file:
		return _error_response("EIO", "Failed to create script file: " + script_path,
			"Error: " + str(FileAccess.get_open_error()))

	file.store_string(content)
	file.close()

	# Refresh the filesystem so Godot sees the new file
	EditorInterface.get_resource_filesystem().scan()

	return _success_response({
		"ok": true,
		"path": script_path,
		"extends": extends_class,
		"exports_count": exports.size(),
		"methods_count": methods.size(),
		"signals_count": signals_list.size(),
	})


func _handle_script_read(params: Dictionary) -> Dictionary:
	## Read a GDScript file content.
	var script_path: String = params.get("path", "")

	if script_path.is_empty():
		return _error_response("EARG", "path is required", "")

	if not script_path.begins_with("res://"):
		script_path = "res://" + script_path.lstrip("/")

	if not FileAccess.file_exists(script_path):
		return _error_response("ESCRIPT_NOT_FOUND", "Script not found: " + script_path,
			"Ensure the script exists at the specified res:// path.")

	var file := FileAccess.open(script_path, FileAccess.READ)
	if not file:
		return _error_response("EIO", "Failed to read script: " + script_path,
			"Error: " + str(FileAccess.get_open_error()))

	var content := file.get_as_text()
	var lines := content.split("\n")
	file.close()

	# Try to parse extends class
	var extends_class := ""
	for line in lines:
		var stripped := line.strip_edges()
		if stripped.begins_with("extends "):
			extends_class = stripped.substr(8).strip_edges()
			break

	return _success_response({
		"path": script_path,
		"content": content,
		"line_count": lines.size(),
		"extends": extends_class,
	})


func _handle_script_edit(params: Dictionary) -> Dictionary:
	## Edit a GDScript file - replace lines or entire file.
	var script_path: String = params.get("path", "")
	var new_content: String = params.get("content", "")
	var line_start: int = params.get("line_start", 0)  # 1-based, 0 = replace entire file
	var line_end: int = params.get("line_end", 0)  # 1-based, inclusive
	var dry_run: bool = params.get("dry_run", false)

	if script_path.is_empty():
		return _error_response("EARG", "path is required", "")

	if new_content.is_empty():
		return _error_response("EARG", "content is required", "")

	if not script_path.begins_with("res://"):
		script_path = "res://" + script_path.lstrip("/")

	if not FileAccess.file_exists(script_path):
		return _error_response("ESCRIPT_NOT_FOUND", "Script not found: " + script_path,
			"Ensure the script exists at the specified res:// path.")

	# Read current content
	var file := FileAccess.open(script_path, FileAccess.READ)
	if not file:
		return _error_response("EIO", "Failed to read script: " + script_path,
			"Error: " + str(FileAccess.get_open_error()))

	var old_content := file.get_as_text()
	var old_lines := old_content.split("\n")
	file.close()

	var result_content: String
	var changes_description: String

	if line_start <= 0:
		# Replace entire file
		result_content = new_content
		changes_description = "Replaced entire file"
	else:
		# Replace specific lines
		if line_start < 1 or line_start > old_lines.size():
			return _error_response("EARG",
				"line_start %d out of range (file has %d lines)" % [line_start, old_lines.size()], "")

		if line_end < line_start:
			line_end = line_start  # Replace single line

		if line_end > old_lines.size():
			line_end = old_lines.size()

		# Build new content
		var new_lines := new_content.split("\n")
		var result_lines: PackedStringArray = []

		# Lines before the edit (1-indexed, so line_start-1 is the index)
		for i in range(line_start - 1):
			result_lines.append(old_lines[i])

		# New content
		for line in new_lines:
			result_lines.append(line)

		# Lines after the edit
		for i in range(line_end, old_lines.size()):
			result_lines.append(old_lines[i])

		result_content = "\n".join(result_lines)
		changes_description = "Replaced lines %d-%d with %d new lines" % [line_start, line_end, new_lines.size()]

	# Dry run - return what would happen
	if dry_run:
		var result_lines := result_content.split("\n")
		return _success_response({
			"dry_run": true,
			"path": script_path,
			"changes": changes_description,
			"old_line_count": old_lines.size(),
			"new_line_count": result_lines.size(),
			"preview": result_content.substr(0, 500) + ("..." if result_content.length() > 500 else ""),
		})

	# Write the file
	file = FileAccess.open(script_path, FileAccess.WRITE)
	if not file:
		return _error_response("EIO", "Failed to write script: " + script_path,
			"Error: " + str(FileAccess.get_open_error()))

	file.store_string(result_content)
	file.close()

	# Refresh the filesystem so Godot sees the changes
	EditorInterface.get_resource_filesystem().scan()

	# Try to reload the script if it's loaded
	if ResourceLoader.exists(script_path):
		var script := load(script_path)
		if script:
			script.reload()

	var result_lines := result_content.split("\n")
	return _success_response({
		"ok": true,
		"path": script_path,
		"changes": changes_description,
		"old_line_count": old_lines.size(),
		"new_line_count": result_lines.size(),
	})


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


func _is_valid_identifier(name: String) -> bool:
	## Check if a string is a valid GDScript identifier.
	## Must start with letter or underscore, followed by letters, digits, or underscores.
	if name.is_empty():
		return false
	var first_char := name[0]
	if not (first_char == "_" or (first_char >= "a" and first_char <= "z") or (first_char >= "A" and first_char <= "Z")):
		return false
	for i in range(1, name.length()):
		var c := name[i]
		if not (c == "_" or (c >= "a" and c <= "z") or (c >= "A" and c <= "Z") or (c >= "0" and c <= "9")):
			return false
	return true


func _is_valid_args(args: String) -> bool:
	## Check if a method args string contains only safe characters.
	## Allows: identifiers, types, colons, commas, spaces, equals, default values.
	## Rejects: newlines, parentheses (except for type hints), quotes for code injection.
	if args.is_empty():
		return true
	# Reject obvious injection attempts
	if args.contains("\n") or args.contains("\r"):
		return false
	if args.contains(";"):  # Statement separator
		return false
	if args.contains("func "):  # Function definition
		return false
	if args.contains("var "):  # Variable definition outside of args context
		return false
	# Allow basic parameter syntax: name: Type = default
	var valid_chars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_:,= .[]()"
	for c in args:
		if not c in valid_chars:
			return false
	return true
