package lite

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
)

// Keep this in parity with skillmain.formatValidationErrors.
// Extract shared validation formatting before allowing behavior to diverge.
func formatValidationErrors(command string, input any, errs validator.ValidationErrors) *skillerr.Error {
	var fields []string
	for _, e := range errs {
		fields = append(fields, formatFieldError(e, input))
	}

	hint := "Check the input fields and ensure all required values are provided"
	if command != "" {
		hint += ". For examples, run: foxctl run " + command + " --examples"
	}

	return skillerr.Validation(
		fmt.Sprintf("input validation failed: %s", strings.Join(fields, ", ")),
		skillerr.WithHint(hint),
		skillerr.WithData("fields", fields),
	)
}

func formatFieldError(e validator.FieldError, input any) string {
	field := formatFieldPath(e, input)
	if field == "" {
		field = e.Field()
	}
	tag := e.Tag()

	switch tag {
	case "required":
		return fmt.Sprintf("%s is required", field)
	case "min":
		return fmt.Sprintf("%s must be at least %s", field, e.Param())
	case "max":
		return fmt.Sprintf("%s must be at most %s", field, e.Param())
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", field, e.Param())
	case "email":
		return fmt.Sprintf("%s must be a valid email", field)
	case "url":
		return fmt.Sprintf("%s must be a valid URL", field)
	default:
		return fmt.Sprintf("%s failed %s validation", field, tag)
	}
}

func formatFieldPath(e validator.FieldError, input any) string {
	if input == nil {
		return ""
	}

	t := reflect.TypeOf(input)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return ""
	}

	path := e.StructNamespace()
	if path == "" {
		return ""
	}
	if name := t.Name(); name != "" {
		path = strings.TrimPrefix(path, name+".")
	}

	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return ""
	}

	var out []string
	for _, part := range parts {
		fieldName, indexSuffix := splitIndex(part)
		if fieldName == "" {
			continue
		}
		jsonName, nextType := jsonFieldName(t, fieldName)
		if jsonName == "" {
			jsonName = fieldName
		}
		out = append(out, jsonName+indexSuffix)
		t = nextType
	}

	return strings.Join(out, ".")
}

func splitIndex(part string) (string, string) {
	idx := strings.Index(part, "[")
	if idx == -1 {
		return part, ""
	}
	return part[:idx], part[idx:]
}

func jsonFieldName(t reflect.Type, fieldName string) (string, reflect.Type) {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return "", nil
	}
	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
		for t != nil && t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
	}
	if t == nil || t.Kind() != reflect.Struct {
		return "", nil
	}

	field, ok := t.FieldByName(fieldName)
	if !ok {
		return "", nil
	}

	tag := field.Tag.Get("json")
	tagName := strings.Split(tag, ",")[0]
	if tagName == "-" {
		tagName = ""
	}

	nextType := field.Type
	for nextType != nil && nextType.Kind() == reflect.Pointer {
		nextType = nextType.Elem()
	}
	if nextType != nil && (nextType.Kind() == reflect.Slice || nextType.Kind() == reflect.Array) {
		nextType = nextType.Elem()
	}
	for nextType != nil && nextType.Kind() == reflect.Pointer {
		nextType = nextType.Elem()
	}

	return tagName, nextType
}
