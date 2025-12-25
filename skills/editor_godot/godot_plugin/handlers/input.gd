## Input handler for GodotAIBridge - handles input action operations.
extends RefCounted
class_name InputHandler


func _init() -> void:
	pass


func handle(action: String, params: Dictionary) -> Dictionary:
	match action:
		"input_action_list":
			return _handle_input_action_list(params)
		"input_action_add":
			return _handle_input_action_add(params)
		"input_action_remove":
			return _handle_input_action_remove(params)
		_:
			return _error_response("EARG", "InputHandler: Unknown action: " + action, "")


func _handle_input_action_list(_params: Dictionary) -> Dictionary:
	## List all input actions.
	var actions: Array[Dictionary] = []

	for action_name in InputMap.get_actions():
		var events: Array[String] = []
		for event in InputMap.action_get_events(action_name):
			events.append(_event_to_string(event))

		actions.append({
			"name": action_name,
			"events": events,
			"deadzone": InputMap.action_get_deadzone(action_name),
		})

	return _success_response({
		"actions": actions,
		"count": actions.size(),
	})


func _handle_input_action_add(params: Dictionary) -> Dictionary:
	## Add an input action or add an event to an existing action.
	var action_name: String = params.get("action", "")
	var event_type: String = params.get("event", "")
	var key: String = params.get("key", "")

	if action_name.is_empty():
		return _error_response("EARG", "action is required", "")
	if event_type.is_empty():
		return _error_response("EARG", "event is required (key, mouse_button, joypad_button)", "")

	# Create action if it doesn't exist
	var created_action := false
	if not InputMap.has_action(action_name):
		InputMap.add_action(action_name)
		created_action = true

	# Create the input event
	var input_event: InputEvent = _create_input_event(event_type, key)
	if input_event == null:
		if created_action:
			InputMap.erase_action(action_name)
		return _error_response("EARG", "Invalid event type or key: " + event_type + " / " + key,
			"Valid event types: key, mouse_button, joypad_button. Keys: KEY_SPACE, KEY_A, etc.")

	# Add the event to the action
	InputMap.action_add_event(action_name, input_event)

	# Save to project settings
	_save_input_map_to_project_settings()

	return _success_response({
		"ok": true,
		"action": action_name,
		"created_action": created_action,
		"added_event": _event_to_string(input_event),
	})


func _handle_input_action_remove(params: Dictionary) -> Dictionary:
	## Remove an input action.
	var action_name: String = params.get("action", "")

	if action_name.is_empty():
		return _error_response("EARG", "action is required", "")

	if not InputMap.has_action(action_name):
		return _error_response("ENOTFOUND", "Input action not found: " + action_name, "")

	# Get events before removing (for response)
	var events: Array[String] = []
	for event in InputMap.action_get_events(action_name):
		events.append(_event_to_string(event))

	InputMap.erase_action(action_name)

	# Save to project settings
	_save_input_map_to_project_settings()

	return _success_response({
		"ok": true,
		"action": action_name,
		"removed_events": events,
	})


func _create_input_event(event_type: String, key: String) -> InputEvent:
	## Create an input event from type and key strings.
	match event_type.to_lower():
		"key":
			var event := InputEventKey.new()
			var keycode := _parse_keycode(key)
			if keycode == KEY_NONE:
				return null
			event.keycode = keycode
			return event

		"mouse_button":
			var event := InputEventMouseButton.new()
			var button := _parse_mouse_button(key)
			if button == MOUSE_BUTTON_NONE:
				return null
			event.button_index = button
			return event

		"joypad_button":
			var event := InputEventJoypadButton.new()
			var button := _parse_joypad_button(key)
			if button < 0:
				return null
			event.button_index = button
			return event

	return null


func _parse_keycode(key: String) -> Key:
	## Parse a key string like "KEY_SPACE" to a Key enum value.
	var upper := key.to_upper()

	# Remove KEY_ prefix if present
	if upper.begins_with("KEY_"):
		upper = upper.substr(4)

	# Common keys
	match upper:
		"SPACE": return KEY_SPACE
		"ENTER", "RETURN": return KEY_ENTER
		"ESCAPE", "ESC": return KEY_ESCAPE
		"TAB": return KEY_TAB
		"BACKSPACE": return KEY_BACKSPACE
		"UP": return KEY_UP
		"DOWN": return KEY_DOWN
		"LEFT": return KEY_LEFT
		"RIGHT": return KEY_RIGHT
		"SHIFT": return KEY_SHIFT
		"CTRL", "CONTROL": return KEY_CTRL
		"ALT": return KEY_ALT

	# Single letter keys
	if upper.length() == 1:
		var code := upper.unicode_at(0)
		if code >= 65 and code <= 90:  # A-Z
			return code

	# Number keys
	if upper.length() == 1 and upper.is_valid_int():
		return upper.unicode_at(0)

	# F-keys
	if upper.begins_with("F") and upper.substr(1).is_valid_int():
		var num := upper.substr(1).to_int()
		if num >= 1 and num <= 12:
			return KEY_F1 + num - 1

	return KEY_NONE


func _parse_mouse_button(key: String) -> MouseButton:
	var upper := key.to_upper()
	if upper.begins_with("MOUSE_BUTTON_"):
		upper = upper.substr(13)

	match upper:
		"LEFT", "1": return MOUSE_BUTTON_LEFT
		"RIGHT", "2": return MOUSE_BUTTON_RIGHT
		"MIDDLE", "3": return MOUSE_BUTTON_MIDDLE
		"WHEEL_UP", "4": return MOUSE_BUTTON_WHEEL_UP
		"WHEEL_DOWN", "5": return MOUSE_BUTTON_WHEEL_DOWN

	return MOUSE_BUTTON_NONE


func _parse_joypad_button(key: String) -> int:
	var upper := key.to_upper()
	if upper.begins_with("JOY_BUTTON_"):
		upper = upper.substr(11)

	match upper:
		"A", "0": return JOY_BUTTON_A
		"B", "1": return JOY_BUTTON_B
		"X", "2": return JOY_BUTTON_X
		"Y", "3": return JOY_BUTTON_Y
		"LEFT_SHOULDER", "LB", "4": return JOY_BUTTON_LEFT_SHOULDER
		"RIGHT_SHOULDER", "RB", "5": return JOY_BUTTON_RIGHT_SHOULDER
		"START", "7": return JOY_BUTTON_START
		"BACK", "SELECT", "6": return JOY_BUTTON_BACK

	if upper.is_valid_int():
		return upper.to_int()

	return -1


func _event_to_string(event: InputEvent) -> String:
	if event is InputEventKey:
		return "Key: " + OS.get_keycode_string(event.keycode)
	elif event is InputEventMouseButton:
		return "MouseButton: " + str(event.button_index)
	elif event is InputEventJoypadButton:
		return "JoypadButton: " + str(event.button_index)
	elif event is InputEventJoypadMotion:
		return "JoypadAxis: " + str(event.axis)
	return event.get_class()


func _save_input_map_to_project_settings() -> void:
	## Save the current InputMap to project settings.
	# This is a simplified version - proper implementation would rebuild
	# the input/ settings section
	ProjectSettings.save()


func _success_response(data: Dictionary) -> Dictionary:
	return {"status": "success", "data": data, "error": null}


func _error_response(code: String, message: String, hint: String) -> Dictionary:
	return {"status": "error", "data": null, "error": {"code": code, "message": message, "hint": hint}}
