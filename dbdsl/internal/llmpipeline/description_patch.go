package llmpipeline

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DescriptionPatch is what a correction returns: the elements it adds or
// replaces, each whole (a thing with all its properties, links and states; a
// rule; a query …), and the references of the elements it removes. Everything
// the patch does not name stays exactly as it was, so the model never copies
// the rest of the description — copying a long description was where
// corrections broke (merged objects, lost elements, exhausted output budget).
type DescriptionPatch struct {
	Upsert DescriptionPatchUpsert `json:"upsert"`
	Remove []string               `json:"remove"`
}

// DescriptionPatchUpsert holds whole elements keyed by their IDs: an ID that
// exists replaces that element in place, a new ID appends one. Boundaries have
// no IDs and are not part of a patch.
type DescriptionPatchUpsert struct {
	Actors        []DescriptionActor    `json:"actors"`
	Things        []DescriptionThing    `json:"things"`
	Rules         []DescriptionRule     `json:"rules"`
	Queries       []DescriptionQuery    `json:"queries"`
	Imports       []DescriptionImport   `json:"imports"`
	Excluded      []DescriptionExcluded `json:"excluded"`
	OpenQuestions []DescriptionQuestion `json:"open_questions"`
}

func descriptionPatchSchema() map[string]any {
	elements := conceptualDescriptionSchema()["properties"].(map[string]any)
	upsert := map[string]any{}
	for _, key := range []string{"actors", "things", "rules", "queries", "imports", "excluded", "open_questions"} {
		upsert[key] = elements[key]
	}
	return object(map[string]any{"upsert": object(upsert), "remove": array(str())})
}

// applyDescriptionPatch returns the current description with the patch
// applied, leaving current untouched. Removal references use the grammar of
// description_refs: a bare thing ID, or "actor:", "rule:", "query:",
// "import:", "question:", "excluded:" with the element's ID.
func applyDescriptionPatch(current ConceptualDescription, patch DescriptionPatch) (ConceptualDescription, []string) {
	var out ConceptualDescription
	raw, err := json.Marshal(current)
	if err == nil {
		err = json.Unmarshal(raw, &out)
	}
	if err != nil {
		return current, []string{"the current description cannot be copied: " + err.Error()}
	}
	errs := []string{}
	upserted := map[string]bool{}
	note := func(kind, id string) string {
		if kind == "thing" {
			return id
		}
		return kind + ":" + id
	}
	out.Actors = upsertByID(out.Actors, patch.Upsert.Actors, func(v DescriptionActor) string { return v.ID }, "actor", note, upserted, &errs)
	out.Things = upsertByID(out.Things, patch.Upsert.Things, func(v DescriptionThing) string { return v.ID }, "thing", note, upserted, &errs)
	out.Rules = upsertByID(out.Rules, patch.Upsert.Rules, func(v DescriptionRule) string { return v.ID }, "rule", note, upserted, &errs)
	out.Queries = upsertByID(out.Queries, patch.Upsert.Queries, func(v DescriptionQuery) string { return v.ID }, "query", note, upserted, &errs)
	out.Imports = upsertByID(out.Imports, patch.Upsert.Imports, func(v DescriptionImport) string { return v.ID }, "import", note, upserted, &errs)
	out.Excluded = upsertByID(out.Excluded, patch.Upsert.Excluded, func(v DescriptionExcluded) string { return v.Segment }, "excluded", note, upserted, &errs)
	out.OpenQuestions = upsertByID(out.OpenQuestions, patch.Upsert.OpenQuestions, func(v DescriptionQuestion) string { return v.ID }, "question", note, upserted, &errs)

	for _, ref := range patch.Remove {
		ref = strings.TrimSpace(ref)
		if upserted[ref] {
			errs = append(errs, fmt.Sprintf("patch both replaces and removes %q", ref))
			continue
		}
		kind, id, prefixed := strings.Cut(ref, ":")
		if !prefixed {
			kind, id = "thing", ref
		}
		removed := false
		switch kind {
		case "thing":
			out.Things, removed = removeByID(out.Things, id, func(v DescriptionThing) string { return v.ID })
		case "actor":
			out.Actors, removed = removeByID(out.Actors, id, func(v DescriptionActor) string { return v.ID })
		case "rule":
			out.Rules, removed = removeByID(out.Rules, id, func(v DescriptionRule) string { return v.ID })
		case "query":
			out.Queries, removed = removeByID(out.Queries, id, func(v DescriptionQuery) string { return v.ID })
		case "import":
			out.Imports, removed = removeByID(out.Imports, id, func(v DescriptionImport) string { return v.ID })
		case "question":
			out.OpenQuestions, removed = removeByID(out.OpenQuestions, id, func(v DescriptionQuestion) string { return v.ID })
		case "excluded":
			out.Excluded, removed = removeByID(out.Excluded, id, func(v DescriptionExcluded) string { return v.Segment })
		}
		if !removed {
			errs = append(errs, fmt.Sprintf("patch removes %q, which is not in the description", ref))
		}
	}
	return out, errs
}

func upsertByID[T any](items, patch []T, id func(T) string, kind string, note func(kind, id string) string, upserted map[string]bool, errs *[]string) []T {
	for _, item := range patch {
		key := strings.TrimSpace(id(item))
		if key == "" {
			*errs = append(*errs, fmt.Sprintf("patch has a %s without an ID", kind))
			continue
		}
		ref := note(kind, key)
		if upserted[ref] {
			*errs = append(*errs, fmt.Sprintf("patch names %q twice", ref))
			continue
		}
		upserted[ref] = true
		replaced := false
		for i := range items {
			if id(items[i]) == key {
				items[i] = item
				replaced = true
				break
			}
		}
		if !replaced {
			items = append(items, item)
		}
	}
	return items
}

func removeByID[T any](items []T, key string, id func(T) string) ([]T, bool) {
	for i := range items {
		if id(items[i]) == key {
			return append(items[:i:i], items[i+1:]...), true
		}
	}
	return items, false
}
