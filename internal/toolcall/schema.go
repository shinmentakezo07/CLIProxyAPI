package toolcall

import "strings"

func MapChoiceFromGeminiMode(mode string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(mode)) {
	case "NONE":
		return "none", true
	case "AUTO":
		return "auto", true
	case "ANY":
		return "required", true
	default:
		return "", false
	}
}

// NormalizeToolChoiceFromGeminiMode is kept as backward-compatible alias.
func NormalizeToolChoiceFromGeminiMode(mode string) (string, bool) {
	return MapChoiceFromGeminiMode(mode)
}

func NormalizeToolSchema(parametersRaw, parametersJSONSchemaRaw string) string {
	if strings.TrimSpace(parametersRaw) != "" {
		return parametersRaw
	}
	return parametersJSONSchemaRaw
}

// PickToolSchema is kept as backward-compatible alias.
func PickToolSchema(parametersRaw, parametersJSONSchemaRaw string) string {
	return NormalizeToolSchema(parametersRaw, parametersJSONSchemaRaw)
}
