## Shader handler for GodotAIBridge - handles shader file operations.
extends RefCounted
class_name ShaderHandler


func _init() -> void:
	pass


func handle(action: String, params: Dictionary) -> Dictionary:
	match action:
		"shader_create":
			return _handle_shader_create(params)
		"shader_edit":
			return _handle_shader_edit(params)
		_:
			return _error_response("EACTION", "ShaderHandler: Unknown action: " + action, "")


func _handle_shader_create(params: Dictionary) -> Dictionary:
	## Create a new shader file with a template.
	var shader_path: String = params.get("path", "")
	var shader_type: String = params.get("type", "canvas_item")  # spatial, canvas_item, particles, sky, fog
	var overwrite: bool = params.get("overwrite", false)

	if shader_path.is_empty():
		return _error_response("EARG", "path is required", "")

	if not shader_path.begins_with("res://"):
		shader_path = "res://" + shader_path.lstrip("/")

	if not shader_path.ends_with(".gdshader"):
		shader_path += ".gdshader"

	# Check if file already exists
	if FileAccess.file_exists(shader_path) and not overwrite:
		return _error_response("ESHADER_EXISTS", "Shader already exists: " + shader_path,
			"Use overwrite=true to replace the existing shader.")

	# Generate shader template
	var content := _generate_shader_template(shader_type)

	# Create directory if needed
	var dir_path := shader_path.get_base_dir()
	if not DirAccess.dir_exists_absolute(dir_path):
		var err := DirAccess.make_dir_recursive_absolute(dir_path)
		if err != OK:
			return _error_response("EIO", "Failed to create directory: " + dir_path, "")

	# Write the file
	var file := FileAccess.open(shader_path, FileAccess.WRITE)
	if not file:
		return _error_response("EIO", "Failed to create shader file: " + shader_path,
			"Error: " + str(FileAccess.get_open_error()))

	file.store_string(content)
	file.close()

	# Refresh filesystem
	EditorInterface.get_resource_filesystem().scan()

	return _success_response({
		"ok": true,
		"path": shader_path,
		"type": shader_type,
	})


func _handle_shader_edit(params: Dictionary) -> Dictionary:
	## Edit a shader file.
	var shader_path: String = params.get("path", "")
	var new_content: String = params.get("content", "")
	var dry_run: bool = params.get("dry_run", false)

	if shader_path.is_empty():
		return _error_response("EARG", "path is required", "")
	if new_content.is_empty():
		return _error_response("EARG", "content is required", "")

	if not shader_path.begins_with("res://"):
		shader_path = "res://" + shader_path.lstrip("/")

	if not FileAccess.file_exists(shader_path):
		return _error_response("ESHADER_NOT_FOUND", "Shader not found: " + shader_path, "")

	# Read current content
	var file := FileAccess.open(shader_path, FileAccess.READ)
	if not file:
		return _error_response("EIO", "Failed to read shader: " + shader_path, "")

	var old_content := file.get_as_text()
	file.close()

	if dry_run:
		return _success_response({
			"dry_run": true,
			"path": shader_path,
			"old_line_count": old_content.split("\n").size(),
			"new_line_count": new_content.split("\n").size(),
		})

	# Write new content
	file = FileAccess.open(shader_path, FileAccess.WRITE)
	if not file:
		return _error_response("EIO", "Failed to write shader: " + shader_path, "")

	file.store_string(new_content)
	file.close()

	# Refresh filesystem
	EditorInterface.get_resource_filesystem().scan()

	return _success_response({
		"ok": true,
		"path": shader_path,
		"old_line_count": old_content.split("\n").size(),
		"new_line_count": new_content.split("\n").size(),
	})


func _generate_shader_template(shader_type: String) -> String:
	match shader_type.to_lower():
		"spatial":
			return """shader_type spatial;

// Uniforms
uniform vec4 albedo : source_color = vec4(1.0);
uniform float roughness : hint_range(0.0, 1.0) = 0.5;
uniform float metallic : hint_range(0.0, 1.0) = 0.0;

void vertex() {
	// Vertex shader code here
}

void fragment() {
	ALBEDO = albedo.rgb;
	ROUGHNESS = roughness;
	METALLIC = metallic;
}
"""
		"canvas_item":
			return """shader_type canvas_item;

// Uniforms
uniform vec4 modulate : source_color = vec4(1.0);

void vertex() {
	// Vertex shader code here
}

void fragment() {
	vec4 color = texture(TEXTURE, UV);
	COLOR = color * modulate;
}
"""
		"particles":
			return """shader_type particles;

uniform float emission_rate : hint_range(0.0, 100.0) = 10.0;

void start() {
	// Initialize particle here
}

void process() {
	// Update particle here
}
"""
		"sky":
			return """shader_type sky;

uniform vec4 sky_color : source_color = vec4(0.4, 0.6, 0.9, 1.0);
uniform vec4 horizon_color : source_color = vec4(0.8, 0.8, 0.9, 1.0);

void sky() {
	vec3 direction = EYEDIR;
	float blend = smoothstep(-0.2, 0.5, direction.y);
	COLOR = mix(horizon_color.rgb, sky_color.rgb, blend);
}
"""
		"fog":
			return """shader_type fog;

uniform float density : hint_range(0.0, 1.0) = 0.1;
uniform vec4 fog_color : source_color = vec4(0.5, 0.5, 0.5, 1.0);

void fog() {
	DENSITY = density;
	ALBEDO = fog_color.rgb;
}
"""
		_:
			return """shader_type canvas_item;

void fragment() {
	COLOR = texture(TEXTURE, UV);
}
"""


func _success_response(data: Dictionary) -> Dictionary:
	return {"status": "success", "data": data, "error": null}


func _error_response(code: String, message: String, hint: String) -> Dictionary:
	return {"status": "error", "data": null, "error": {"code": code, "message": message, "hint": hint}}
