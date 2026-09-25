package workspace

import (
	"context"
	"os"
	"strings"

	"dbdsl/internal/llm"
	"dbdsl/internal/llmpipeline"
)

type LLMExecutionProfile struct {
	Provider          string         `json:"provider"`
	Model             string         `json:"model"`
	ReasoningEffort   string         `json:"reasoning_effort"`
	MaxOutputTokens   int            `json:"max_output_tokens"`
	MaxRepairAttempts int            `json:"max_repair_attempts"`
	MaxParallelism    int            `json:"max_parallelism"`
	PromptVersion     string         `json:"prompt_version"`
	PolicyVersion     string         `json:"policy_version"`
	BudgetPolicy      string         `json:"budget_policy,omitempty"`
	ContextPolicy     string         `json:"context_policy,omitempty"`
	CallGatePolicy    string         `json:"call_gate_policy,omitempty"`
	RiskPolicy        string         `json:"risk_policy,omitempty"`
	StageOutputLimits map[string]int `json:"stage_output_limits,omitempty"`
	// OutputLanguage is detected from the sources when they are processed and
	// frozen so every later stage writes in the same language.
	OutputLanguage string `json:"output_language,omitempty"`
}

type llmExecutionControls struct {
	MaxParallelism    int
	MaxRepairAttempts int
	PromptVersion     string
}

func defaultLLMExecutionProfile(model string) LLMExecutionProfile {
	if model == "" {
		model = llm.DefaultModel
	}
	effort := llm.DefaultReasoningEffort
	if configured := strings.TrimSpace(os.Getenv("DBDSL_LLM_REASONING_EFFORT")); configured != "" {
		effort = configured
	}
	provider := "openai"
	if model == "mock-model" {
		provider = "mock"
	}
	return LLMExecutionProfile{
		Provider: provider, Model: model, ReasoningEffort: effort,
		MaxOutputTokens: llm.DefaultMaxOutputTokens, MaxRepairAttempts: 2, MaxParallelism: 6,
		PromptVersion: llmpipeline.PromptTemplateVersion, PolicyVersion: "design_obligations/v0.7.2+review_semantics/v2",
		BudgetPolicy: "adaptive_v1", ContextPolicy: "minimal_context_v1", CallGatePolicy: "semantic_need_v1",
		RiskPolicy: "review_risk_value_v1",
		StageOutputLimits: map[string]int{
			"source_segmentation": 16000, "requirement_atoms": 16000, "functional_analysis": 12000,
			"crud_mapping": 16000, "review_candidates": 14000, "conceptual_model": 32000,
			"logical_model": 40000, "repair": 20000,
		},
	}
}

// ProjectOutputLanguage returns the language human-readable LLM output must use:
// DBDSL_OUTPUT_LANGUAGE overrides, then the frozen profile, then detection
// from the project's extracted sources (for projects processed before this).
func (s *Store) ProjectOutputLanguage(projectID string) string {
	if configured := strings.TrimSpace(os.Getenv("DBDSL_OUTPUT_LANGUAGE")); configured != "" {
		return configured
	}
	project, ok := s.Project(projectID)
	if !ok {
		return llmpipeline.LanguageEnglish
	}
	if project.LLMExecutionProfile != nil && project.LLMExecutionProfile.OutputLanguage != "" {
		return project.LLMExecutionProfile.OutputLanguage
	}
	return s.detectSourceLanguage(projectID)
}

func (s *Store) detectSourceLanguage(projectID string) string {
	if configured := strings.TrimSpace(os.Getenv("DBDSL_OUTPUT_LANGUAGE")); configured != "" {
		return configured
	}
	return llmpipeline.DetectSourceLanguage(s.projectSourceSample(projectID))
}

// projectSourceSample concatenates up to 20 KB of extracted source text.
func (s *Store) projectSourceSample(projectID string) string {
	resources, err := s.Resources(projectID)
	if err != nil {
		return ""
	}
	var sample strings.Builder
	for _, resource := range resources {
		if resource.ExtractedTextPath == "" || sample.Len() > 20000 {
			continue
		}
		if data, readErr := os.ReadFile(s.absoluteWorkspacePath(resource.ExtractedTextPath)); readErr == nil {
			sample.Write(data)
			sample.WriteByte('\n')
		}
	}
	return sample.String()
}

// LanguageContext attaches the project's output language to an LLM stage context.
func (s *Store) LanguageContext(ctx context.Context, projectID string) context.Context {
	return llmpipeline.WithOutputLanguage(ctx, s.ProjectOutputLanguage(projectID))
}

func (s *Store) resolveLLMOptions(projectID, stage, model, effort string, maxOutputTokens int) (string, string, int) {
	project, ok := s.Project(projectID)
	profile := defaultLLMExecutionProfile(model)
	if ok && project.LLMExecutionProfile != nil {
		profile = *project.LLMExecutionProfile
	}
	if model == "" {
		model = profile.Model
	}
	if effort == "" {
		effort = profile.ReasoningEffort
	}
	if maxOutputTokens <= 0 && profile.BudgetPolicy != "adaptive_v1" {
		maxOutputTokens = profile.MaxOutputTokens
		if stage == "conceptual_model" && maxOutputTokens < 24000 {
			maxOutputTokens = 24000
		}
		if stage == "logical_model" && maxOutputTokens < 32000 {
			maxOutputTokens = 32000
		}
	}
	return model, effort, maxOutputTokens
}

func (s *Store) resolveLLMExecutionControls(projectID string) llmExecutionControls {
	profile := defaultLLMExecutionProfile("")
	if project, ok := s.Project(projectID); ok && project.LLMExecutionProfile != nil {
		profile = *project.LLMExecutionProfile
	}
	if profile.MaxParallelism <= 0 {
		profile.MaxParallelism = 1
	}
	if profile.MaxRepairAttempts < 0 {
		profile.MaxRepairAttempts = 0
	}
	if profile.PromptVersion == "" {
		profile.PromptVersion = llmpipeline.PromptTemplateVersion
	}
	return llmExecutionControls{
		MaxParallelism: profile.MaxParallelism, MaxRepairAttempts: profile.MaxRepairAttempts,
		PromptVersion: profile.PromptVersion,
	}
}

type stageBudgetInput struct {
	Sentences, Units, Atoms, Areas, ReviewClusters, RequiredObligations int
	Entities, Relationships, Constraints, Issues, AffectedElements      int
}

func (s *Store) resolveStageBudget(projectID, stage string, requested int, input stageBudgetInput) int {
	project, ok := s.Project(projectID)
	profile := defaultLLMExecutionProfile("")
	if ok && project.LLMExecutionProfile != nil {
		profile = *project.LLMExecutionProfile
	}
	if requested > 0 {
		return requested
	}
	value, floor, ceiling := 0, 6000, profile.StageOutputLimits[stage]
	switch stage {
	case "source_segmentation":
		value, floor = 3000+70*input.Sentences, 6000
	case "requirement_atoms":
		value, floor = 3000+220*input.Units, 8000
	case "functional_analysis":
		value, floor = 2500+70*input.Atoms, 5000
	case "crud_mapping":
		value, floor = 3000+100*input.Atoms+250*input.Areas, 6000
	case "review_candidates":
		value, floor, ceiling = 3000+350*input.ReviewClusters, 3000, 8000
	case "conceptual_model":
		value, floor = 6000+180*input.RequiredObligations, 12000
		if input.Units > 0 {
			// Segment-based flow: the whole description is one response.
			value, floor = 8000+150*input.Units, 16000
		}
	case "logical_model":
		value, floor = 8000+350*input.Entities+250*input.Relationships+120*input.Constraints, 16000
	default:
		value, floor, ceiling = 6000+500*input.Issues+300*input.AffectedElements, 6000, profile.StageOutputLimits["repair"]
	}
	if ceiling <= 0 {
		ceiling = profile.MaxOutputTokens
	}
	if ceiling < floor {
		ceiling = floor
	}
	if value < floor {
		value = floor
	}
	if value > ceiling {
		value = ceiling
	}
	return value
}
