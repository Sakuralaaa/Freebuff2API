package main

import "testing"

func TestNormalizeToolSchemas_ResolvesRefsAndNullable(t *testing.T) {
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": "query",
				"parameters": map[string]any{
					"type": "object",
					"definitions": map[string]any{
						"UserQuery": map[string]any{
							"type": []any{"object", "null"},
							"properties": map[string]any{
								"keyword": map[string]any{
									"anyOf": []any{
										map[string]any{"type": "string"},
										map[string]any{"type": "null"},
									},
								},
							},
						},
					},
					"properties": map[string]any{
						"input": map[string]any{
							"$ref": "#/definitions/UserQuery",
						},
					},
				},
			},
		},
	}

	normalizeToolSchemas(tools)

	tool := tools[0].(map[string]any)
	function := tool["function"].(map[string]any)
	params := function["parameters"].(map[string]any)
	if _, ok := params["definitions"]; ok {
		t.Fatalf("definitions should be removed after normalization")
	}

	properties := params["properties"].(map[string]any)
	input := properties["input"].(map[string]any)
	if _, hasRef := input["$ref"]; hasRef {
		t.Fatalf("$ref should be resolved")
	}
	if input["type"] != "object" {
		t.Fatalf("expected input type to normalize to object, got %v", input["type"])
	}
	keyword := input["properties"].(map[string]any)["keyword"].(map[string]any)
	if keyword["type"] != "string" {
		t.Fatalf("expected keyword type string after nullable simplify, got %v", keyword["type"])
	}
}

func TestNormalizeToolSchemas_DeduplicatesEnumAndDropsNullConst(t *testing.T) {
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": "mode",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"level": map[string]any{
							"type":  []any{"null", "string"},
							"enum":  []any{"fast", "fast", nil, "safe"},
							"const": nil,
						},
					},
				},
			},
		},
	}

	normalizeToolSchemas(tools)

	level := tools[0].(map[string]any)["function"].(map[string]any)["parameters"].(map[string]any)["properties"].(map[string]any)["level"].(map[string]any)
	if level["type"] != "string" {
		t.Fatalf("expected type string, got %v", level["type"])
	}
	if _, ok := level["const"]; ok {
		t.Fatalf("null const should be removed")
	}
	enum := level["enum"].([]any)
	if len(enum) != 2 || enum[0] != "fast" || enum[1] != "safe" {
		t.Fatalf("unexpected normalized enum: %#v", enum)
	}
}
