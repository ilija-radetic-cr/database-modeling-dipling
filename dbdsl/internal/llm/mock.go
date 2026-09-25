package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type MockClient struct {
	Structured map[string]json.RawMessage
	Text       map[string]string
}

func NewDefaultMockClient() *MockClient {
	return &MockClient{
		Structured: map[string]json.RawMessage{
			"source_segmentation": json.RawMessage(`{}`),
		},
		Text: map[string]string{
			"baseline_dbml": defaultBaselineDBML,
			"baseline_sql":  defaultBaselineSQL,
		},
	}
}

func (m *MockClient) GenerateStructured(ctx context.Context, req Request) (Response, error) {
	_ = ctx
	if m == nil {
		m = NewDefaultMockClient()
	}
	payload, ok := m.Structured[req.Stage]
	if req.Stage == "source_segmentation" && (!ok || string(payload) == `{}`) {
		var err error
		payload, err = dynamicSourceSegmentationPayload(req.Input)
		if err != nil {
			return Response{}, err
		}
		ok = true
	}
	if req.Stage == "conceptual_description" && !ok {
		var err error
		payload, err = dynamicConceptualDescriptionPayload(req.Input)
		if err != nil {
			return Response{}, err
		}
		ok = true
	}
	if !ok {
		return Response{}, fmt.Errorf("mock LLM has no structured response for stage %s", req.Stage)
	}
	raw := fmt.Sprintf(`{"id":"mock_%s","status":"completed","output_text":%q}`, req.Stage, string(payload))
	return Response{
		Provider: "mock",
		Model:    nonEmpty(req.Model, "mock"),
		Raw:      raw,
		Text:     string(payload),
		Parsed:   payload,
	}, nil
}

var mockSegmentLine = regexp.MustCompile(`^\s*(SU-[0-9]+(?:\.[0-9]+)?) \[([^\]]+)\]`)

// dynamicConceptualDescriptionPayload builds a small but structurally complete
// description from the numbered segments: two things linked 1:N, a lifecycle,
// a rule, and every remaining segment excluded, so offline runs cover the whole
// segment-based flow.
func dynamicConceptualDescriptionPayload(input string) (json.RawMessage, error) {
	ids := []string{}
	for _, line := range strings.Split(input, "\n") {
		match := mockSegmentLine.FindStringSubmatch(line)
		if match == nil || strings.HasPrefix(match[2], "heading") {
			continue
		}
		ids = append(ids, match[1])
	}
	if len(ids) == 0 {
		return nil, errors.New("mock conceptual description needs at least one non-heading segment")
	}
	evidence := func(id string) map[string]any { return map[string]any{"segments": []string{id}, "mode": "direct"} }
	first, second := ids[0], ids[0]
	if len(ids) > 1 {
		second = ids[1]
	}
	property := func(name, meaning, presence string, allowed []string, id string) map[string]any {
		if allowed == nil {
			allowed = []string{}
		}
		valueType := "text"
		if name == "kolicina" {
			valueType = "integer"
		}
		return map[string]any{"name": name, "meaning": meaning, "value_type": valueType, "shape": "single", "parts": []string{}, "presence": presence, "condition": "", "variant": "",
			"allowed_values": allowed, "origin": "entered", "source": "", "evidence": evidence(id)}
	}
	excluded := []map[string]string{}
	for _, id := range ids[min(2, len(ids)):] {
		excluded = append(excluded, map[string]string{"segment": id, "reason": "other"})
	}
	payload := map[string]any{
		"actors": []map[string]any{{"id": "korisnik", "name": "Korisnik", "description": "Korisnik sistema.", "represented_by": "", "differs_by": "", "evidence": evidence(first)}},
		"things": []map[string]any{
			{"id": "zapis", "name": "Zapis", "kind": "object", "description": "Glavni zapis sistema.", "identified_by": []string{"oznaka"}, "instance_of": "", "variants": []string{}, "created_when": "",
				"properties": []map[string]any{property("oznaka", "Jedinstvena oznaka zapisa.", "required", nil, first), property("naziv", "Naziv zapisa.", "required", nil, first)},
				"links":      []map[string]any{}, "states": []string{"nov", "zavrsen"},
				"transitions": []map[string]any{{"from": "nov", "to": "zavrsen", "trigger": "završetak", "by": "korisnik", "effects": "", "evidence": evidence(first)}},
				"evidence":    evidence(first)},
			{"id": "stavka", "name": "Stavka", "kind": "record", "description": "Stavka zapisa.", "identified_by": []string{}, "instance_of": "", "variants": []string{}, "created_when": "",
				"properties": []map[string]any{property("kolicina", "Količina stavke.", "required", nil, second)},
				"links":      []map[string]any{{"to": "zapis", "meaning": "pripada zapisu", "per_this": "1", "per_other": "0..N", "evidence": evidence(second)}},
				"states":     []string{}, "transitions": []map[string]any{}, "evidence": evidence(second)},
		},
		"rules":   []map[string]any{{"id": "kolicina_pozitivna", "kind": "quantity", "statement": "Količina je pozitivna.", "applies_to": []string{"stavka.kolicina"}, "parameters": []string{}, "evidence": evidence(second)}},
		"queries": []map[string]any{{"id": "pretraga_zapisa", "description": "Pretraga zapisa po nazivu.", "needs": []string{"zapis.naziv"}, "criteria": []string{"zapis.naziv"}, "evidence": evidence(first)}},
		"imports": []map[string]any{}, "boundaries": []map[string]any{},
		"excluded": excluded, "open_questions": []map[string]any{},
	}
	data, err := json.Marshal(payload)
	return json.RawMessage(data), err
}

func dynamicSourceSegmentationPayload(input string) (json.RawMessage, error) {
	document := input
	if start := strings.Index(document, "<document>"); start >= 0 {
		document = document[start+len("<document>"):]
	}
	if end := strings.LastIndex(document, "</document>"); end >= 0 {
		document = document[:end]
	}
	segments := []map[string]any{}
	appendSegment := func(typeName, text string) {
		segments = append(segments, map[string]any{"type": typeName, "text": text})
	}
	plainLines := []string{}
	flushPlain := func() {
		if len(plainLines) == 0 {
			return
		}
		for _, sentence := range splitMockSentences(strings.Join(plainLines, " ")) {
			appendSegment("sentence", sentence)
		}
		plainLines = nil
	}
	for _, line := range strings.Split(document, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		typeName := ""
		switch {
		case strings.HasPrefix(trimmed, "#"):
			typeName = "heading"
		case strings.Contains(lower, "универзитет у београду") || strings.Contains(lower, "univerzitet u beogradu"):
			typeName = "page_header"
		case strings.Trim(trimmed, "0123456789") == "":
			typeName = "page_number"
		case strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "*"):
			typeName = "list"
		case strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "["):
			typeName = "example"
		}
		if typeName == "" {
			plainLines = append(plainLines, trimmed)
			continue
		}
		flushPlain()
		appendSegment(typeName, trimmed)
	}
	flushPlain()
	data, err := json.Marshal(map[string]any{"segments": segments})
	return json.RawMessage(data), err
}

func splitMockSentences(value string) []string {
	var out []string
	var current strings.Builder
	for _, r := range strings.TrimSpace(value) {
		current.WriteRune(r)
		if r == '.' || r == '!' || r == '?' {
			if sentence := strings.TrimSpace(current.String()); sentence != "" {
				out = append(out, sentence)
			}
			current.Reset()
		}
	}
	if sentence := strings.TrimSpace(current.String()); sentence != "" {
		out = append(out, sentence)
	}
	return out
}

func (m *MockClient) GenerateText(ctx context.Context, req TextRequest) (TextResponse, error) {
	_ = ctx
	if m == nil {
		m = NewDefaultMockClient()
	}
	key := "baseline_" + req.Metadata["target"]
	text, ok := m.Text[key]
	if !ok {
		return TextResponse{}, fmt.Errorf("mock LLM has no text response for %s", key)
	}
	raw := fmt.Sprintf(`{"id":"mock_%s","status":"completed","output_text":%q}`, req.Stage, text)
	return TextResponse{
		Provider: "mock",
		Model:    nonEmpty(req.Model, "mock"),
		Raw:      raw,
		Text:     text,
	}, nil
}

const defaultBaselineDBML = `Table products {
  id integer [pk]
  name varchar
}`

const defaultBaselineSQL = `CREATE TABLE products (
  id BIGINT PRIMARY KEY,
  name VARCHAR(255)
);`
