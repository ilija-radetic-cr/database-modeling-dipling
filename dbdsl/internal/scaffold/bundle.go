package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"dbdsl/internal/dsl"

	"gopkg.in/yaml.v3"
)

type Options struct {
	ModelID string
	Name    string
}

type Result struct {
	ModelPath string
	Files     []string
}

type sourceUnitDraft struct {
	ID         string
	Section    string
	Line       int
	Exact      string
	Normalized string
}

func BundleFromTask(taskPath, outDir string, options Options) (Result, error) {
	taskBytes, err := os.ReadFile(taskPath)
	if err != nil {
		return Result{}, fmt.Errorf("read task: %w", err)
	}
	return bundleFromText(string(taskBytes), outDir, options, taskPath)
}

func BundleFromText(taskText, outDir string, options Options) (Result, error) {
	return bundleFromText(taskText, outDir, options, "TASK.md")
}

func bundleFromText(taskText, outDir string, options Options, sourcePath string) (Result, error) {
	taskText = strings.TrimSpace(taskText)
	if taskText == "" {
		return Result{}, fmt.Errorf("task text is empty")
	}

	title := nonEmpty(options.Name, titleFromTask(taskText, sourcePath))
	modelID := slug(nonEmpty(options.ModelID, title))
	units := sourceUnitsFromTask(taskText)
	if len(units) == 0 {
		units = []sourceUnitDraft{{
			ID:         "RAW-SU-001",
			Section:    "task",
			Line:       1,
			Exact:      title,
			Normalized: title,
		}}
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create output directory: %w", err)
	}

	taskFileName := "TASK.md"
	files := map[string]any{
		taskFileName:            taskText + "\n",
		"source_units.yaml":     buildSourceUnitsFile(taskFileName, title, units),
		"review_decisions.yaml": buildReviewDecisionsFile(),
		"db_model.dsl.yaml":     buildModelDocument(modelID, title, taskFileName, units),
	}

	names := []string{
		taskFileName,
		"source_units.yaml",
		"review_decisions.yaml",
		"db_model.dsl.yaml",
	}
	result := Result{ModelPath: filepath.Join(outDir, "db_model.dsl.yaml")}
	for _, name := range names {
		path := filepath.Join(outDir, name)
		var data []byte
		if text, ok := files[name].(string); ok {
			data = []byte(text)
		} else {
			marshaled, err := yaml.Marshal(files[name])
			if err != nil {
				return Result{}, fmt.Errorf("marshal %s: %w", name, err)
			}
			data = marshaled
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return Result{}, fmt.Errorf("write %s: %w", name, err)
		}
		result.Files = append(result.Files, path)
	}
	return result, nil
}

func buildSourceUnitsFile(taskFileName, title string, units []sourceUnitDraft) dsl.SourceUnitsFile {
	out := dsl.SourceUnitsFile{
		Document: dsl.SourceUnitsDocument{
			ID:              slug(title) + "_source_units",
			Title:           title + " source units",
			PipelineVersion: "0.6",
			SourceFile:      taskFileName,
			SourceLanguage:  "unknown",
			Granularity:     "sentence",
		},
	}
	for _, unit := range units {
		out.SourceUnits = append(out.SourceUnits, dsl.SourceUnit{
			ID:        unit.ID,
			Kind:      "sentence",
			Section:   unit.Section,
			Location:  fmt.Sprintf("%s#line-%d", taskFileName, unit.Line),
			Relevance: "model_relevant",
			Tags:      []string{"raw_task"},
			Text: dsl.SourceUnitText{
				Exact:      unit.Exact,
				Normalized: unit.Normalized,
			},
		})
	}
	return out
}

func buildReviewDecisionsFile() dsl.ReviewDecisionsFile {
	return dsl.ReviewDecisionsFile{
		Document: map[string]any{
			"id":                      "raw_task_review_decisions",
			"title":                   "Raw task review decisions",
			"pipeline_version":        "0.6",
			"generation_strategy":     "deterministic_scaffold",
			"requires_domain_review":  true,
			"domain_review_completed": false,
		},
		ReviewState: map[string]any{
			"status":                           "scaffold_review_not_started",
			"all_required_reviews_resolved":    true,
			"unresolved_requires_review_flags": 0,
		},
		ReviewDecisions: []dsl.ReviewDecision{},
		CoverageChecks: []map[string]any{{
			"id":     "scaffold_has_no_blocking_reviews",
			"status": "passed",
			"note":   "The scaffold is importable; domain modeling review is still required.",
		}},
	}
}

func buildModelDocument(modelID, title, taskFileName string, units []sourceUnitDraft) dsl.Document {
	allSources := sourceUnitIDs(units)
	requireTrue := true
	requireFalse := false
	return dsl.Document{
		DSL: dsl.DSLMeta{
			Name:    "DB-DSL",
			Version: "0.6",
		},
		Model: dsl.ModelInfo{
			ID:          modelID,
			Name:        title + " Scaffold",
			DomainSlice: modelID,
			Status:      "scaffold_requires_review",
			Description: "Importable v0.6 scaffold generated from raw task text. Replace the generic requirement-capture schema with domain entities before treating DBML as final.",
		},
		Source: dsl.SourceInfo{
			PipelineVersion:     "0.6",
			TaskTextFile:        taskFileName,
			SourceUnitsFile:     "source_units.yaml",
			ReviewDecisionsFile: "review_decisions.yaml",
			ReviewState:         "scaffold_review_not_started",
			DerivationStrategy:  "deterministic_scaffold_from_raw_task_text",
		},
		Entities: []dsl.Entity{
			{
				ID:          "SourceDocument",
				Label:       "Source Document",
				Description: "Raw PIA task document captured for traceability.",
				TableName:   "source_documents",
				Kind:        "regular",
				Evidence:    evidence(allSources),
				Attributes: []dsl.Attribute{
					attribute("slug", "Slug", "Stable source document slug.", "string", true, allSources),
					attribute("title", "Title", "Source document title.", "string", true, allSources),
					attribute("source_file", "Source File", "Original task file path inside the bundle.", "string", true, allSources),
				},
			},
			{
				ID:          "RequirementItem",
				Label:       "Requirement Item",
				Description: "Reviewable source-derived requirement statement.",
				TableName:   "requirement_items",
				Kind:        "regular",
				Evidence:    evidence(allSources),
				Attributes: []dsl.Attribute{
					attribute("source_unit_id", "Source Unit ID", "Source unit that produced the requirement item.", "string", true, allSources),
					attribute("statement", "Statement", "Normalized requirement statement.", "text", true, allSources),
					attribute("modeling_relevance", "Modeling Relevance", "Initial modeling relevance assigned by the scaffold.", "string", true, allSources),
					{
						ID:          "review_status",
						Label:       "Review Status",
						Description: "Manual review state for the scaffolded requirement item.",
						Type:        "string",
						Required:    &requireTrue,
						EnumValues:  []string{"draft", "reviewed", "accepted"},
						Evidence:    evidence(allSources),
					},
				},
			},
		},
		Relationships: []dsl.Relationship{{
			ID:          "RequirementItemDocument",
			Label:       "Requirement item document",
			Description: "Each requirement item belongs to the source document from which it was extracted.",
			From:        "RequirementItem",
			To:          "SourceDocument",
			Cardinality: "many_to_one",
			Required:    &requireTrue,
			FKRequired:  &requireTrue,
			OnDelete:    "cascade",
			Identifying: &requireFalse,
			Evidence:    evidence(allSources),
		}},
		Constraints: []dsl.Constraint{
			{
				ID:          "source_document_slug_unique",
				Type:        "unique",
				Owner:       "SourceDocument",
				Field:       "slug",
				Description: "Each captured source document has a unique slug.",
				Evidence:    evidence(allSources),
			},
			{
				ID:          "requirement_item_source_unit_unique",
				Type:        "unique",
				Owner:       "RequirementItem",
				Field:       "source_unit_id",
				Description: "The scaffold creates at most one requirement item per source unit.",
				Evidence:    evidence(allSources),
			},
		},
		ImportSpecs:   []dsl.ImportSpec{},
		StateMachines: []dsl.StateMachine{},
		DerivedViews:  []dsl.DerivedView{},
		FileSpecs:     []dsl.FileSpec{},
	}
}

func attribute(id, label, description, typ string, required bool, sourceUnits []string) dsl.Attribute {
	return dsl.Attribute{
		ID:          id,
		Label:       label,
		Description: description,
		Type:        typ,
		Required:    &required,
		Evidence:    evidence(sourceUnits),
	}
}

func evidence(sourceUnits []string) dsl.Evidence {
	return dsl.Evidence{
		SourceUnits:     append([]string(nil), sourceUnits...),
		ReviewDecisions: []string{},
		SupportLevel:    "explicit",
		Confidence:      "medium",
	}
}

func sourceUnitsFromTask(taskText string) []sourceUnitDraft {
	var units []sourceUnitDraft
	section := "task"
	lines := strings.Split(taskText, "\n")
	for lineNumber, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			section = slug(strings.TrimSpace(strings.TrimLeft(trimmed, "#")))
			continue
		}
		for _, sentence := range splitSentences(trimmed) {
			if len([]rune(sentence)) < 12 {
				continue
			}
			units = append(units, sourceUnitDraft{
				ID:         fmt.Sprintf("RAW-SU-%03d", len(units)+1),
				Section:    section,
				Line:       lineNumber + 1,
				Exact:      sentence,
				Normalized: normalizeSpace(sentence),
			})
		}
	}
	return units
}

var sentencePattern = regexp.MustCompile(`[^.!?]+[.!?]?`)

func splitSentences(line string) []string {
	matches := sentencePattern.FindAllString(line, -1)
	if len(matches) == 0 {
		return []string{strings.TrimSpace(line)}
	}
	var out []string
	for _, match := range matches {
		if trimmed := strings.TrimSpace(match); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func titleFromTask(taskText, taskPath string) string {
	for _, line := range strings.Split(taskText, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			return strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		}
	}
	base := strings.TrimSuffix(filepath.Base(taskPath), filepath.Ext(taskPath))
	return strings.ReplaceAll(base, "_", " ")
}

func sourceUnitIDs(units []sourceUnitDraft) []string {
	ids := make([]string, 0, len(units))
	for _, unit := range units {
		ids = append(ids, unit.ID)
	}
	return ids
}

func slug(value string) string {
	value = strings.ToLower(value)
	var out []rune
	lastUnderscore := false
	for _, r := range value {
		if r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			out = append(out, r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			out = append(out, '_')
			lastUnderscore = true
		}
	}
	slugged := strings.Trim(string(out), "_")
	if slugged == "" {
		return "pia_task"
	}
	if slugged[0] >= '0' && slugged[0] <= '9' {
		return "task_" + slugged
	}
	return slugged
}

func normalizeSpace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}
