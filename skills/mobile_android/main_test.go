package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "mobile/android", command)
}

func TestAllowedOps(t *testing.T) {
	expectedOps := []string{
		"list_devices",
		"device_info",
		"install",
		"launch",
		"terminate",
		"screenshot",
		"tap",
		"swipe",
		"type_text",
		"press_key",
		"ui_tree",
		"logs",
		"logcat_filter",
		"logcat_app",
		"logcat_crash",
		"logcat_clear",
		"open_url",
		"grant_permission",
		"record_screen",
		"record_stop",
		"dumpsys",
		"pull_file",
		"push_file",
	}
	assert.Equal(t, expectedOps, allowedOps)
}

func TestAllowedOpsCount(t *testing.T) {
	assert.Len(t, allowedOps, 23)
}

func TestRecordPIDFile(t *testing.T) {
	assert.Equal(t, "/tmp/agentctl_android_record.pid", androidRecordPIDFile)
}

func TestRecordPathFile(t *testing.T) {
	assert.Equal(t, "/tmp/agentctl_android_record_path.txt", androidRecordPathFile)
}

func TestRecordSerialFile(t *testing.T) {
	assert.Equal(t, "/tmp/agentctl_android_record_serial.txt", androidRecordSerialFile)
}

// Tests for input structure

func TestInput_AllFields(t *testing.T) {
	in := input{
		Operation:  "tap",
		Serial:     "emulator-5554",
		App:        "com.example.app",
		Activity:   ".MainActivity",
		X:          100,
		Y:          200,
		X2:         300,
		Y2:         400,
		Text:       "hello world",
		URL:        "https://example.com",
		Keycode:    "KEYCODE_HOME",
		Permission: "android.permission.CAMERA",
		Tag:        "MyApp",
		Level:      "E",
		Service:    "activity",
		RemotePath: "/sdcard/file.txt",
		LocalPath:  "/tmp/file.txt",
		Output:     "/tmp/screenshot.png",
		Duration:   500,
		Count:      100,
		Pattern:    "error",
		Since:      "1h",
	}

	assert.Equal(t, "tap", in.Operation)
	assert.Equal(t, "emulator-5554", in.Serial)
	assert.Equal(t, "com.example.app", in.App)
	assert.Equal(t, ".MainActivity", in.Activity)
	assert.Equal(t, 100, in.X)
	assert.Equal(t, 200, in.Y)
	assert.Equal(t, 300, in.X2)
	assert.Equal(t, 400, in.Y2)
	assert.Equal(t, "hello world", in.Text)
	assert.Equal(t, "https://example.com", in.URL)
	assert.Equal(t, "KEYCODE_HOME", in.Keycode)
	assert.Equal(t, "android.permission.CAMERA", in.Permission)
	assert.Equal(t, "MyApp", in.Tag)
	assert.Equal(t, "E", in.Level)
	assert.Equal(t, "activity", in.Service)
	assert.Equal(t, "/sdcard/file.txt", in.RemotePath)
	assert.Equal(t, "/tmp/file.txt", in.LocalPath)
	assert.Equal(t, "/tmp/screenshot.png", in.Output)
	assert.Equal(t, 500, in.Duration)
	assert.Equal(t, 100, in.Count)
	assert.Equal(t, "error", in.Pattern)
	assert.Equal(t, "1h", in.Since)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := input{
		Operation: "screenshot",
		Serial:    "emulator-5554",
		Output:    "/tmp/test.png",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Operation, decoded.Operation)
	assert.Equal(t, in.Serial, decoded.Serial)
	assert.Equal(t, in.Output, decoded.Output)
}

func TestInput_EmptyFields(t *testing.T) {
	in := input{}

	assert.Empty(t, in.Operation)
	assert.Empty(t, in.Serial)
	assert.Empty(t, in.App)
	assert.Empty(t, in.Activity)
	assert.Zero(t, in.X)
	assert.Zero(t, in.Y)
	assert.Zero(t, in.X2)
	assert.Zero(t, in.Y2)
	assert.Empty(t, in.Text)
	assert.Empty(t, in.URL)
	assert.Empty(t, in.Keycode)
	assert.Empty(t, in.Permission)
	assert.Empty(t, in.Tag)
	assert.Empty(t, in.Level)
	assert.Empty(t, in.Service)
	assert.Empty(t, in.RemotePath)
	assert.Empty(t, in.LocalPath)
	assert.Empty(t, in.Output)
	assert.Zero(t, in.Duration)
	assert.Zero(t, in.Count)
	assert.Empty(t, in.Pattern)
	assert.Empty(t, in.Since)
}

func TestInput_OperationValues(t *testing.T) {
	for _, op := range allowedOps {
		in := input{Operation: op}
		assert.Equal(t, op, in.Operation)
	}
}

func TestInput_JSONFieldNames(t *testing.T) {
	in := input{
		Operation:  "tap",
		Serial:     "s",
		App:        "a",
		Activity:   "act",
		X:          1,
		Y:          2,
		X2:         3,
		Y2:         4,
		Text:       "t",
		URL:        "u",
		Keycode:    "k",
		Permission: "p",
		Tag:        "tag",
		Level:      "l",
		Service:    "srv",
		RemotePath: "r",
		LocalPath:  "loc",
		Output:     "o",
		Duration:   10,
		Count:      20,
		Pattern:    "pat",
		Since:      "1h",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "operation")
	assert.Contains(t, jsonStr, "serial")
	assert.Contains(t, jsonStr, "app")
	assert.Contains(t, jsonStr, "activity")
	assert.Contains(t, jsonStr, `"x":`)
	assert.Contains(t, jsonStr, `"y":`)
	assert.Contains(t, jsonStr, "x2")
	assert.Contains(t, jsonStr, "y2")
	assert.Contains(t, jsonStr, "text")
	assert.Contains(t, jsonStr, "url")
	assert.Contains(t, jsonStr, "keycode")
	assert.Contains(t, jsonStr, "permission")
	assert.Contains(t, jsonStr, "tag")
	assert.Contains(t, jsonStr, "level")
	assert.Contains(t, jsonStr, "service")
	assert.Contains(t, jsonStr, "remote_path")
	assert.Contains(t, jsonStr, "local_path")
	assert.Contains(t, jsonStr, "output")
	assert.Contains(t, jsonStr, "duration")
	assert.Contains(t, jsonStr, "count")
	assert.Contains(t, jsonStr, "pattern")
	assert.Contains(t, jsonStr, "since")
}

func TestInput_OmitEmptyFields(t *testing.T) {
	in := input{
		Operation: "list_devices",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	// Only operation should be present (others are omitempty)
	assert.Contains(t, jsonStr, "operation")
	assert.NotContains(t, jsonStr, "serial")
	assert.NotContains(t, jsonStr, "app")
}

// Tests for UINode structure

func TestUINode_AllFields(t *testing.T) {
	node := UINode{
		Index:         "0",
		Text:          "Click me",
		ResourceID:    "com.example:id/button",
		Class:         "android.widget.Button",
		Package:       "com.example.app",
		ContentDesc:   "Submit button",
		Checkable:     "false",
		Checked:       "false",
		Clickable:     "true",
		Enabled:       "true",
		Focusable:     "true",
		Focused:       "false",
		Scrollable:    "false",
		LongClickable: "false",
		Password:      "false",
		Selected:      "false",
		Bounds:        "[0,0][100,50]",
	}

	assert.Equal(t, "0", node.Index)
	assert.Equal(t, "Click me", node.Text)
	assert.Equal(t, "com.example:id/button", node.ResourceID)
	assert.Equal(t, "android.widget.Button", node.Class)
	assert.Equal(t, "com.example.app", node.Package)
	assert.Equal(t, "Submit button", node.ContentDesc)
	assert.Equal(t, "false", node.Checkable)
	assert.Equal(t, "false", node.Checked)
	assert.Equal(t, "true", node.Clickable)
	assert.Equal(t, "true", node.Enabled)
	assert.Equal(t, "true", node.Focusable)
	assert.Equal(t, "false", node.Focused)
	assert.Equal(t, "false", node.Scrollable)
	assert.Equal(t, "false", node.LongClickable)
	assert.Equal(t, "false", node.Password)
	assert.Equal(t, "false", node.Selected)
	assert.Equal(t, "[0,0][100,50]", node.Bounds)
}

func TestUINode_EmptyFields(t *testing.T) {
	node := UINode{}

	assert.Empty(t, node.Index)
	assert.Empty(t, node.Text)
	assert.Empty(t, node.ResourceID)
	assert.Empty(t, node.Class)
	assert.Empty(t, node.Package)
	assert.Empty(t, node.ContentDesc)
	assert.Empty(t, node.Checkable)
	assert.Empty(t, node.Checked)
	assert.Empty(t, node.Clickable)
	assert.Empty(t, node.Enabled)
	assert.Empty(t, node.Focusable)
	assert.Empty(t, node.Focused)
	assert.Empty(t, node.Scrollable)
	assert.Empty(t, node.LongClickable)
	assert.Empty(t, node.Password)
	assert.Empty(t, node.Selected)
	assert.Empty(t, node.Bounds)
	assert.Nil(t, node.Children)
}

func TestUINode_WithChildren(t *testing.T) {
	child1 := UINode{Text: "Child 1", Index: "0"}
	child2 := UINode{Text: "Child 2", Index: "1"}
	parent := UINode{
		Text:     "Parent",
		Index:    "0",
		Children: []UINode{child1, child2},
	}

	assert.Len(t, parent.Children, 2)
	assert.Equal(t, "Child 1", parent.Children[0].Text)
	assert.Equal(t, "Child 2", parent.Children[1].Text)
}

func TestUINode_NestedChildren(t *testing.T) {
	grandchild := UINode{Text: "Grandchild", Index: "0"}
	child := UINode{
		Text:     "Child",
		Index:    "0",
		Children: []UINode{grandchild},
	}
	parent := UINode{
		Text:     "Parent",
		Index:    "0",
		Children: []UINode{child},
	}

	assert.Len(t, parent.Children, 1)
	assert.Len(t, parent.Children[0].Children, 1)
	assert.Equal(t, "Grandchild", parent.Children[0].Children[0].Text)
}

// Tests for parseUIHierarchy helper

func TestParseUIHierarchy_Empty(t *testing.T) {
	result := parseUIHierarchy([]byte{})
	assert.Empty(t, result)
}

func TestParseUIHierarchy_InvalidXML(t *testing.T) {
	result := parseUIHierarchy([]byte("not xml"))
	assert.Empty(t, result)
}

func TestParseUIHierarchy_EmptyHierarchy(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?><hierarchy></hierarchy>`
	result := parseUIHierarchy([]byte(xml))
	assert.Empty(t, result)
}

func TestParseUIHierarchy_SingleNode(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<hierarchy>
  <node index="0" text="Hello" resource-id="com.example:id/text" class="android.widget.TextView"
        package="com.example" content-desc="" checkable="false" checked="false" clickable="true"
        enabled="true" focusable="false" focused="false" scrollable="false" long-clickable="false"
        password="false" selected="false" bounds="[0,0][100,50]"/>
</hierarchy>`
	result := parseUIHierarchy([]byte(xml))
	assert.Len(t, result, 1)
	assert.Equal(t, "0", result[0]["index"])
	assert.Equal(t, "Hello", result[0]["text"])
	assert.Equal(t, "com.example:id/text", result[0]["resource_id"])
	assert.Equal(t, "android.widget.TextView", result[0]["class"])
	assert.Equal(t, true, result[0]["clickable"])
	assert.Equal(t, true, result[0]["enabled"])
}

func TestParseUIHierarchy_MultipleNodes(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<hierarchy>
  <node index="0" text="Button 1" class="android.widget.Button" clickable="true" enabled="true" bounds="[0,0][100,50]"/>
  <node index="1" text="Button 2" class="android.widget.Button" clickable="true" enabled="true" bounds="[0,50][100,100]"/>
</hierarchy>`
	result := parseUIHierarchy([]byte(xml))
	assert.Len(t, result, 2)
	assert.Equal(t, "Button 1", result[0]["text"])
	assert.Equal(t, "Button 2", result[1]["text"])
}

func TestParseUIHierarchy_NestedNodes(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<hierarchy>
  <node index="0" text="Parent" class="android.widget.LinearLayout" bounds="[0,0][100,100]">
    <node index="0" text="Child" class="android.widget.TextView" bounds="[0,0][50,50]"/>
  </node>
</hierarchy>`
	result := parseUIHierarchy([]byte(xml))
	// Flattening should produce 2 elements
	assert.Len(t, result, 2)
	assert.Equal(t, "Parent", result[0]["text"])
	assert.Equal(t, "Child", result[1]["text"])
}

func TestParseUIHierarchy_BooleanAttributes(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<hierarchy>
  <node index="0" text="" checkable="true" checked="true" clickable="false" enabled="false"
        focusable="true" focused="true" scrollable="true" long-clickable="true" password="true" selected="true"
        bounds="[0,0][100,50]"/>
</hierarchy>`
	result := parseUIHierarchy([]byte(xml))
	assert.Len(t, result, 1)
	assert.Equal(t, true, result[0]["checkable"])
	assert.Equal(t, true, result[0]["checked"])
	assert.Equal(t, false, result[0]["clickable"])
	assert.Equal(t, false, result[0]["enabled"])
	assert.Equal(t, true, result[0]["focusable"])
	assert.Equal(t, true, result[0]["focused"])
	assert.Equal(t, true, result[0]["scrollable"])
	assert.Equal(t, true, result[0]["long_clickable"])
	assert.Equal(t, true, result[0]["password"])
	assert.Equal(t, true, result[0]["selected"])
}

// Tests for filterLogsByPattern helper

func TestFilterLogsByPattern_Empty(t *testing.T) {
	result := filterLogsByPattern([]byte{}, "pattern")
	assert.Empty(t, result)
}

func TestFilterLogsByPattern_NoMatch(t *testing.T) {
	logs := []byte("line 1\nline 2\nline 3")
	result := filterLogsByPattern(logs, "nomatch")
	assert.Empty(t, result)
}

func TestFilterLogsByPattern_SimpleMatch(t *testing.T) {
	logs := []byte("error: something failed\ninfo: all good\nerror: another failure")
	result := filterLogsByPattern(logs, "error")
	lines := string(result)
	assert.Contains(t, lines, "error: something failed")
	assert.Contains(t, lines, "error: another failure")
	assert.NotContains(t, lines, "info: all good")
}

func TestFilterLogsByPattern_CaseInsensitive(t *testing.T) {
	logs := []byte("ERROR: uppercase\nerror: lowercase\nError: mixed")
	result := filterLogsByPattern(logs, "error")
	lines := string(result)
	assert.Contains(t, lines, "ERROR: uppercase")
	assert.Contains(t, lines, "error: lowercase")
	assert.Contains(t, lines, "Error: mixed")
}

func TestFilterLogsByPattern_RegexPattern(t *testing.T) {
	logs := []byte("user_123 logged in\nuser_456 logged out\nadmin_789 logged in")
	result := filterLogsByPattern(logs, "user_\\d+")
	lines := string(result)
	assert.Contains(t, lines, "user_123")
	assert.Contains(t, lines, "user_456")
	assert.NotContains(t, lines, "admin_789")
}

func TestFilterLogsByPattern_InvalidRegexFallsBackToSubstring(t *testing.T) {
	logs := []byte("test[bracket\ntest]other\nno match")
	// Invalid regex pattern (unbalanced bracket) should fall back to substring match
	result := filterLogsByPattern(logs, "[bracket")
	lines := string(result)
	assert.Contains(t, lines, "test[bracket")
}

func TestFilterLogsByPattern_AllMatch(t *testing.T) {
	logs := []byte("error 1\nerror 2\nerror 3")
	result := filterLogsByPattern(logs, "error")
	lines := string(result)
	assert.Contains(t, lines, "error 1")
	assert.Contains(t, lines, "error 2")
	assert.Contains(t, lines, "error 3")
}

func TestFilterLogsByPattern_PartialLineMatch(t *testing.T) {
	logs := []byte("2024-01-01 10:00:00 E/MyApp: NullPointerException\n2024-01-01 10:00:01 I/MyApp: Started")
	result := filterLogsByPattern(logs, "NullPointer")
	lines := string(result)
	assert.Contains(t, lines, "NullPointerException")
	assert.NotContains(t, lines, "Started")
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := input{
		Operation:  "launch",
		Serial:     "full-serial",
		App:        "com.full.app",
		Activity:   ".FullActivity",
		X:          10,
		Y:          20,
		X2:         30,
		Y2:         40,
		Text:       "full text",
		URL:        "https://full.com",
		Keycode:    "KEYCODE_BACK",
		Permission: "android.permission.READ_CONTACTS",
		Tag:        "FullTag",
		Level:      "W",
		Service:    "window",
		RemotePath: "/sdcard/full.txt",
		LocalPath:  "/tmp/full.txt",
		Output:     "/full/output.png",
		Duration:   1000,
		Count:      200,
		Pattern:    "full",
		Since:      "2h",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Operation, decoded.Operation)
	assert.Equal(t, in.Serial, decoded.Serial)
	assert.Equal(t, in.App, decoded.App)
	assert.Equal(t, in.Activity, decoded.Activity)
	assert.Equal(t, in.X, decoded.X)
	assert.Equal(t, in.Y, decoded.Y)
	assert.Equal(t, in.X2, decoded.X2)
	assert.Equal(t, in.Y2, decoded.Y2)
	assert.Equal(t, in.Text, decoded.Text)
	assert.Equal(t, in.URL, decoded.URL)
	assert.Equal(t, in.Keycode, decoded.Keycode)
	assert.Equal(t, in.Permission, decoded.Permission)
	assert.Equal(t, in.Tag, decoded.Tag)
	assert.Equal(t, in.Level, decoded.Level)
	assert.Equal(t, in.Service, decoded.Service)
	assert.Equal(t, in.RemotePath, decoded.RemotePath)
	assert.Equal(t, in.LocalPath, decoded.LocalPath)
	assert.Equal(t, in.Output, decoded.Output)
	assert.Equal(t, in.Duration, decoded.Duration)
	assert.Equal(t, in.Count, decoded.Count)
	assert.Equal(t, in.Pattern, decoded.Pattern)
	assert.Equal(t, in.Since, decoded.Since)
}

func TestInput_LogcatOperations(t *testing.T) {
	// logcat_filter
	in1 := input{
		Operation: "logcat_filter",
		Tag:       "MyApp",
		Level:     "E",
		Count:     100,
	}
	assert.Equal(t, "logcat_filter", in1.Operation)
	assert.Equal(t, "MyApp", in1.Tag)
	assert.Equal(t, "E", in1.Level)

	// logcat_app
	in2 := input{
		Operation: "logcat_app",
		App:       "com.example.app",
		Level:     "W",
	}
	assert.Equal(t, "logcat_app", in2.Operation)
	assert.Equal(t, "com.example.app", in2.App)

	// logcat_crash
	in3 := input{
		Operation: "logcat_crash",
		App:       "com.example.app",
	}
	assert.Equal(t, "logcat_crash", in3.Operation)

	// logcat_clear
	in4 := input{
		Operation: "logcat_clear",
	}
	assert.Equal(t, "logcat_clear", in4.Operation)
}

func TestInput_FileOperations(t *testing.T) {
	// pull_file
	in1 := input{
		Operation:  "pull_file",
		RemotePath: "/sdcard/DCIM/photo.jpg",
		LocalPath:  "/tmp/photo.jpg",
	}
	assert.Equal(t, "pull_file", in1.Operation)
	assert.Equal(t, "/sdcard/DCIM/photo.jpg", in1.RemotePath)
	assert.Equal(t, "/tmp/photo.jpg", in1.LocalPath)

	// push_file
	in2 := input{
		Operation:  "push_file",
		LocalPath:  "/tmp/config.json",
		RemotePath: "/sdcard/config.json",
	}
	assert.Equal(t, "push_file", in2.Operation)
	assert.Equal(t, "/tmp/config.json", in2.LocalPath)
	assert.Equal(t, "/sdcard/config.json", in2.RemotePath)
}

func TestInput_KeycodeValues(t *testing.T) {
	keycodes := []string{"KEYCODE_HOME", "KEYCODE_BACK", "KEYCODE_MENU", "KEYCODE_ENTER", "KEYCODE_VOLUME_UP"}

	for _, kc := range keycodes {
		in := input{Keycode: kc}
		assert.Equal(t, kc, in.Keycode)
	}
}

func TestInput_LogLevelValues(t *testing.T) {
	levels := []string{"V", "D", "I", "W", "E", "F", "S"}

	for _, level := range levels {
		in := input{Level: level}
		assert.Equal(t, level, in.Level)
	}
}

func TestInput_SwipeWithDuration(t *testing.T) {
	in := input{
		Operation: "swipe",
		X:         100,
		Y:         500,
		X2:        100,
		Y2:        100,
		Duration:  800,
	}

	assert.Equal(t, 100, in.X)
	assert.Equal(t, 500, in.Y)
	assert.Equal(t, 100, in.X2)
	assert.Equal(t, 100, in.Y2)
	assert.Equal(t, 800, in.Duration)
}

func TestAllowedOps_ContainsLogcatOperations(t *testing.T) {
	assert.Contains(t, allowedOps, "logs")
	assert.Contains(t, allowedOps, "logcat_filter")
	assert.Contains(t, allowedOps, "logcat_app")
	assert.Contains(t, allowedOps, "logcat_crash")
	assert.Contains(t, allowedOps, "logcat_clear")
}

func TestAllowedOps_ContainsFileOperations(t *testing.T) {
	assert.Contains(t, allowedOps, "pull_file")
	assert.Contains(t, allowedOps, "push_file")
}

func TestAllowedOps_ContainsDumpsys(t *testing.T) {
	assert.Contains(t, allowedOps, "dumpsys")
}

func TestAllowedOps_ContainsGrantPermission(t *testing.T) {
	assert.Contains(t, allowedOps, "grant_permission")
}

func TestAllowedOps_ContainsRecordOperations(t *testing.T) {
	assert.Contains(t, allowedOps, "record_screen")
	assert.Contains(t, allowedOps, "record_stop")
}
