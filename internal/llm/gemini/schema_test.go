package gemini_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ayushpatil0810/forge/internal/llm/gemini"
)

func TestSanitizeSchema(t *testing.T) {
	input := `{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"title": "ReadFileSchema",
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"path": {
				"type": "string",
				"description": "File path",
				"default": "main.go"
			}
		},
		"required": ["path"]
	}`

	cleaned := gemini.SanitizeSchema(json.RawMessage(input))
	cleanedStr := string(cleaned)

	if strings.Contains(cleanedStr, "$schema") {
		t.Errorf("cleaned schema should not contain $schema: %s", cleanedStr)
	}
	if strings.Contains(cleanedStr, "additionalProperties") {
		t.Errorf("cleaned schema should not contain additionalProperties: %s", cleanedStr)
	}
	if strings.Contains(cleanedStr, "default") {
		t.Errorf("cleaned schema should not contain default: %s", cleanedStr)
	}
	if strings.Contains(cleanedStr, "title") {
		t.Errorf("cleaned schema should not contain title: %s", cleanedStr)
	}
	if !strings.Contains(cleanedStr, "path") || !strings.Contains(cleanedStr, "required") {
		t.Errorf("cleaned schema lost valid properties: %s", cleanedStr)
	}
}
