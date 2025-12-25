## Console handler for GodotAIBridge - handles console/debug output operations.
extends RefCounted
class_name ConsoleHandler

var _output_buffer: PackedStringArray = []
const MAX_BUFFER_SIZE: int = 1000


func _init() -> void:
	pass


func handle(action: String, params: Dictionary) -> Dictionary:
	match action:
		"console_output":
			return _handle_console_output(params)
		_:
			return _error_response("EACTION", "ConsoleHandler: Unknown action: " + action, "")


func add_output(message: String) -> void:
	## Add a message to the output buffer.
	_output_buffer.append(message)
	if _output_buffer.size() > MAX_BUFFER_SIZE:
		_output_buffer = _output_buffer.slice(-MAX_BUFFER_SIZE)


func _handle_console_output(params: Dictionary) -> Dictionary:
	## Get recent console/debug output.
	var line_count: int = params.get("line_count", 100)
	line_count = mini(line_count, MAX_BUFFER_SIZE)

	var lines: Array[String] = []
	var start_idx := maxi(0, _output_buffer.size() - line_count)
	for i in range(start_idx, _output_buffer.size()):
		lines.append(_output_buffer[i])

	return _success_response({
		"lines": lines,
		"count": lines.size(),
		"total_buffered": _output_buffer.size(),
	})


func _success_response(data: Dictionary) -> Dictionary:
	return {"status": "success", "data": data, "error": null}


func _error_response(code: String, message: String, hint: String) -> Dictionary:
	return {"status": "error", "data": null, "error": {"code": code, "message": message, "hint": hint}}
