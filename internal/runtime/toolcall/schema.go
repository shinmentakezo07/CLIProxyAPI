package toolcall

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type FieldType string

const (
	FieldTypeString  FieldType = "string"
	FieldTypeInteger FieldType = "integer"
	FieldTypeNumber  FieldType = "number"
	FieldTypeBoolean FieldType = "boolean"
	FieldTypeObject  FieldType = "object"
	FieldTypeArray   FieldType = "array"
)

type FieldSchema struct {
	Type        FieldType
	Required    bool
	Enum        []string
	MinInt      *int64
	MaxInt      *int64
	Description string
}

type ObjectSchema struct {
	Fields            map[string]FieldSchema
	AdditionalAllowed bool
}

type ValidationIssue struct {
	Field   string
	Code    string
	Message string
}

type ValidationError struct {
	Issues []ValidationIssue
}

func (e *ValidationError) Error() string {
	if e == nil || len(e.Issues) == 0 {
		return "validation failed"
	}
	first := e.Issues[0]
	if first.Field == "" {
		return first.Message
	}
	return fmt.Sprintf("%s: %s", first.Field, first.Message)
}

func (e *ValidationError) add(field, code, msg string) {
	e.Issues = append(e.Issues, ValidationIssue{Field: field, Code: code, Message: msg})
}

func ParseArgsObject(raw string) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader([]byte(strings.TrimSpace(raw))))
	dec.UseNumber()
	var parsed any
	if err := dec.Decode(&parsed); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("multiple JSON values are not allowed")
	}
	obj, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tool arguments must be a JSON object")
	}
	return obj, nil
}

func (s *ObjectSchema) ValidateRaw(raw string) (map[string]any, error) {
	obj, err := ParseArgsObject(raw)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return obj, nil
	}
	if err := s.Validate(obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func (s *ObjectSchema) Validate(obj map[string]any) error {
	if s == nil {
		return nil
	}
	verr := &ValidationError{}

	for name, field := range s.Fields {
		val, ok := obj[name]
		if !ok {
			if field.Required {
				verr.add(name, "missing_required", "missing required field")
			}
			continue
		}
		validateField(verr, name, val, field)
	}

	if !s.AdditionalAllowed {
		for name := range obj {
			if _, ok := s.Fields[name]; !ok {
				verr.add(name, "additional_property", "field is not allowed")
			}
		}
	}

	if len(verr.Issues) > 0 {
		sort.Slice(verr.Issues, func(i, j int) bool {
			if verr.Issues[i].Field == verr.Issues[j].Field {
				return verr.Issues[i].Code < verr.Issues[j].Code
			}
			return verr.Issues[i].Field < verr.Issues[j].Field
		})
		return verr
	}
	return nil
}

func validateField(verr *ValidationError, name string, val any, schema FieldSchema) {
	if verr == nil {
		return
	}
	if !matchesFieldType(val, schema.Type) {
		verr.add(name, "invalid_type", fmt.Sprintf("expected %s, got %s", schema.Type, valueTypeName(val)))
		return
	}

	if len(schema.Enum) > 0 {
		sv, ok := val.(string)
		if !ok {
			verr.add(name, "invalid_enum_type", "enum validation requires string field")
			return
		}
		if !containsString(schema.Enum, sv) {
			verr.add(name, "enum", fmt.Sprintf("must be one of: %s", strings.Join(schema.Enum, ", ")))
			return
		}
	}

	if schema.Type == FieldTypeInteger {
		n, ok := val.(json.Number)
		if !ok {
			return
		}
		iv, err := n.Int64()
		if err != nil {
			verr.add(name, "invalid_integer", "must be an integer")
			return
		}
		if schema.MinInt != nil && iv < *schema.MinInt {
			verr.add(name, "min", fmt.Sprintf("must be >= %d", *schema.MinInt))
		}
		if schema.MaxInt != nil && iv > *schema.MaxInt {
			verr.add(name, "max", fmt.Sprintf("must be <= %d", *schema.MaxInt))
		}
	}
}

func matchesFieldType(val any, t FieldType) bool {
	switch t {
	case "", FieldTypeObject:
		_, ok := val.(map[string]any)
		return ok
	case FieldTypeString:
		_, ok := val.(string)
		return ok
	case FieldTypeInteger:
		_, ok := val.(json.Number)
		return ok
	case FieldTypeNumber:
		_, ok := val.(json.Number)
		return ok
	case FieldTypeBoolean:
		_, ok := val.(bool)
		return ok
	case FieldTypeArray:
		_, ok := val.([]any)
		return ok
	default:
		return false
	}
}

func valueTypeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case json.Number:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return strings.TrimPrefix(fmt.Sprintf("%T", v), "interface {}")
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func IntPtr(v int64) *int64 { return &v }

func ParseIntArg(m map[string]any, key string) (int64, bool, error) {
	if m == nil {
		return 0, false, nil
	}
	raw, ok := m[key]
	if !ok {
		return 0, false, nil
	}
	n, ok := raw.(json.Number)
	if !ok {
		return 0, true, fmt.Errorf("%s must be integer", key)
	}
	iv, err := strconv.ParseInt(n.String(), 10, 64)
	if err != nil {
		return 0, true, fmt.Errorf("%s must be integer", key)
	}
	return iv, true, nil
}
