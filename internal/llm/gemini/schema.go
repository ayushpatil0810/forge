package gemini

import (
	"encoding/json"
	"strings"
)

// unsupportedSchemaKeys are fields rejected by Gemini's strict schema validator.
var unsupportedSchemaKeys = map[string]bool{
	"$schema":              true,
	"$id":                  true,
	"$ref":                 true,
	"$comment":             true,
	"additionalProperties": true,
	"default":              true,
	"examples":             true,
	"title":                true,
}

// SanitizeSchema strips fields not supported by Gemini from a JSON schema.
func SanitizeSchema(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}

	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return raw
	}

	cleaned := cleanValue(parsed)
	out, err := json.Marshal(cleaned)
	if err != nil {
		return raw
	}
	return json.RawMessage(out)
}

func cleanValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		cleanedMap := make(map[string]any, len(val))
		for k, child := range val {
			if unsupportedSchemaKeys[k] {
				continue
			}
			// Gemini expects uppercase or lowercase types; ensure valid strings
			if k == "type" {
				if typeStr, ok := child.(string); ok {
					cleanedMap[k] = strings.ToLower(typeStr)
					continue
				}
			}
			cleanedMap[k] = cleanValue(child)
		}
		return cleanedMap

	case []any:
		cleanedSlice := make([]any, len(val))
		for i, elem := range val {
			cleanedSlice[i] = cleanValue(elem)
		}
		return cleanedSlice

	default:
		return v
	}
}

