package workspace

import (
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
	provider := "openai"
	if model == "mock-model" {
		provider = "mock"
	}
	return LLMExecutionProfile{
		Provider: provider, Model: model, ReasoningEffort: llm.DefaultReasoningEffort,
		MaxOutputTokens: llm.DefaultMaxOutputTokens, MaxRepairAttempts: 2, MaxParallelism: 3,
		PromptVersion: llmpipeline.PromptTemplateVersion, PolicyVersion: "design_obligations/v0.7.1",
		BudgetPolicy: "adaptive_v1", ContextPolicy: "minimal_context_v1", CallGatePolicy: "semantic_need_v1",
		RiskPolicy: "review_risk_value_v1",
		StageOutputLimits: map[string]int{
			"source_segmentation": 16000, "source_units": 16000, "requirement_atoms": 16000, "functional_analysis": 12000,
			"crud_mapping": 16000, "review_candidates": 14000, "conceptual_model": 32000,
			"logical_model": 40000, "repair": 20000,
		},
	}
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
	case "source_units":
		value, floor = 3000+90*input.Sentences, 6000
	case "requirement_atoms":
		value, floor = 3000+220*input.Units, 8000
	case "functional_analysis":
		value, floor = 2500+70*input.Atoms, 5000
	case "crud_mapping":
		value, floor = 3000+100*input.Atoms+250*input.Areas, 6000
	case "review_candidates":
		value, floor = 2500+500*input.ReviewClusters, 5000
	case "conceptual_model":
		value, floor = 6000+180*input.RequiredObligations, 12000
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
