## Debug handler for GodotAIBridge - handles debug visualization operations.
extends RefCounted
class_name DebugHandler


func _init() -> void:
	pass


func handle(action: String, params: Dictionary) -> Dictionary:
	match action:
		"debug_draw_enable":
			return _handle_debug_draw_enable(params)
		"debug_draw_disable":
			return _handle_debug_draw_disable(params)
		_:
			return _error_response("EARG", "DebugHandler: Unknown action: " + action, "")


func _handle_debug_draw_enable(params: Dictionary) -> Dictionary:
	## Enable debug visualization mode.
	var mode: String = params.get("mode", "wireframe")

	# Map mode string to Viewport.DebugDraw enum
	var debug_draw: int
	match mode.to_lower():
		"disabled", "off":
			debug_draw = Viewport.DEBUG_DRAW_DISABLED
		"unshaded":
			debug_draw = Viewport.DEBUG_DRAW_UNSHADED
		"lighting":
			debug_draw = Viewport.DEBUG_DRAW_LIGHTING
		"overdraw":
			debug_draw = Viewport.DEBUG_DRAW_OVERDRAW
		"wireframe":
			debug_draw = Viewport.DEBUG_DRAW_WIREFRAME
		"normal_buffer":
			debug_draw = Viewport.DEBUG_DRAW_NORMAL_BUFFER
		"voxel_gi_albedo":
			debug_draw = Viewport.DEBUG_DRAW_VOXEL_GI_ALBEDO
		"voxel_gi_lighting":
			debug_draw = Viewport.DEBUG_DRAW_VOXEL_GI_LIGHTING
		"voxel_gi_emission":
			debug_draw = Viewport.DEBUG_DRAW_VOXEL_GI_EMISSION
		"shadow_atlas":
			debug_draw = Viewport.DEBUG_DRAW_SHADOW_ATLAS
		"directional_shadow_atlas":
			debug_draw = Viewport.DEBUG_DRAW_DIRECTIONAL_SHADOW_ATLAS
		"scene_luminance":
			debug_draw = Viewport.DEBUG_DRAW_SCENE_LUMINANCE
		"ssao":
			debug_draw = Viewport.DEBUG_DRAW_SSAO
		"ssil":
			debug_draw = Viewport.DEBUG_DRAW_SSIL
		"pssm_splits":
			debug_draw = Viewport.DEBUG_DRAW_PSSM_SPLITS
		"decal_atlas":
			debug_draw = Viewport.DEBUG_DRAW_DECAL_ATLAS
		"sdfgi":
			debug_draw = Viewport.DEBUG_DRAW_SDFGI
		"sdfgi_probes":
			debug_draw = Viewport.DEBUG_DRAW_SDFGI_PROBES
		"gi_buffer":
			debug_draw = Viewport.DEBUG_DRAW_GI_BUFFER
		"disable_lod":
			debug_draw = Viewport.DEBUG_DRAW_DISABLE_LOD
		"cluster_omni_lights":
			debug_draw = Viewport.DEBUG_DRAW_CLUSTER_OMNI_LIGHTS
		"cluster_spot_lights":
			debug_draw = Viewport.DEBUG_DRAW_CLUSTER_SPOT_LIGHTS
		"cluster_decals":
			debug_draw = Viewport.DEBUG_DRAW_CLUSTER_DECALS
		"cluster_reflection_probes":
			debug_draw = Viewport.DEBUG_DRAW_CLUSTER_REFLECTION_PROBES
		"occluders":
			debug_draw = Viewport.DEBUG_DRAW_OCCLUDERS
		"motion_vectors":
			debug_draw = Viewport.DEBUG_DRAW_MOTION_VECTORS
		"internal_buffer":
			debug_draw = Viewport.DEBUG_DRAW_INTERNAL_BUFFER
		_:
			return _error_response("EARG", "Unknown debug draw mode: " + mode,
				"Available modes: disabled, unshaded, lighting, overdraw, wireframe, normal_buffer, etc.")

	# Apply to editor viewport
	var editor_viewport := EditorInterface.get_editor_viewport_3d(0)
	if not editor_viewport:
		return _error_response("ENOTFOUND", "No 3D editor viewport available",
			"Ensure a 3D scene is open in the editor.")

	editor_viewport.debug_draw = debug_draw

	return _success_response({
		"ok": true,
		"mode": mode,
		"debug_draw_value": debug_draw,
	})


func _handle_debug_draw_disable(_params: Dictionary) -> Dictionary:
	## Disable debug visualization.
	var editor_viewport := EditorInterface.get_editor_viewport_3d(0)
	if not editor_viewport:
		return _error_response("ENOTFOUND", "No 3D editor viewport available",
			"Ensure a 3D scene is open in the editor.")

	editor_viewport.debug_draw = Viewport.DEBUG_DRAW_DISABLED

	return _success_response({
		"ok": true,
		"mode": "disabled",
	})


func _success_response(data: Dictionary) -> Dictionary:
	return {"status": "success", "data": data, "error": null}


func _error_response(code: String, message: String, hint: String) -> Dictionary:
	return {"status": "error", "data": null, "error": {"code": code, "message": message, "hint": hint}}
