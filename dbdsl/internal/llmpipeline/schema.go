package llmpipeline

import "sort"

func sourceSegmentationSchema() map[string]any {
	return object(map[string]any{
		"segments": array(object(map[string]any{
			"type": enum("heading", "sentence", "list", "example", "footnote", "page_header", "page_footer", "page_number", "other"),
			"text": str(),
		})),
	})
}

// ConceptualValueTypes are the DB-DSL scalar types a conceptual attribute may
// declare; the surrogate "id" type is excluded because keys are generated.
var ConceptualValueTypes = []string{"string", "text", "integer", "decimal", "boolean", "date", "time", "datetime", "uuid", "email", "phone", "url", "file_path", "money"}

func object(properties map[string]any) map[string]any {
	required := make([]string, 0, len(properties))
	for key := range properties {
		required = append(required, key)
	}
	sort.Strings(required)
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func array(items any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}

func str() map[string]any {
	return map[string]any{"type": "string"}
}

func enum(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}
