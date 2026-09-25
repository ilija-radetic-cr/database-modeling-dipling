package workspace

import (
	"testing"

	"dbdsl/internal/llmpipeline"
)

func TestProcessSourcesFreezesLLMExecutionProfileForLaterStages(t *testing.T) {
	store := newIngestionTestStore(t)
	project, _ := store.CreateProject("Profile", "", "en", "test")
	_, revision, _ := store.AddPastedTextResource(project.ID, 0, "Task", "Products have names.")
	if _, _, err := store.ProcessSources(project.ID, ProcessSourcesOptions{BaseRevision: revision, Model: "mock-model", ReasoningEffort: "low", MaxOutputTokens: 4000}); err != nil {
		t.Fatalf("process sources: %v", err)
	}
	state, _ := store.Project(project.ID)
	if state.LLMExecutionProfile == nil || state.LLMExecutionProfile.Model != "mock-model" || state.LLMExecutionProfile.MaxRepairAttempts != 2 {
		t.Fatalf("execution profile was not frozen: %+v", state.LLMExecutionProfile)
	}
	model, effort, tokens := store.resolveLLMOptions(project.ID, "conceptual_model", "", "", 0)
	if model != "mock-model" || effort != "medium" || tokens != 0 {
		t.Fatalf("conceptual stage did not use its reasoning default: model=%s effort=%s tokens=%d", model, effort, tokens)
	}
	_, explicitEffort, _ := store.resolveLLMOptions(project.ID, "conceptual_model", "", "high", 0)
	if explicitEffort != "high" {
		t.Fatalf("explicit conceptual reasoning effort was not preserved: %s", explicitEffort)
	}
	_, sourceEffort, _ := store.resolveLLMOptions(project.ID, "source_segmentation", "", "", 0)
	if sourceEffort != "low" {
		t.Fatalf("source segmentation reasoning effort changed: %s", sourceEffort)
	}
	budget := store.resolveStageBudget(project.ID, "conceptual_model", tokens, stageBudgetInput{})
	if budget != 12000 {
		t.Fatalf("conceptual adaptive budget = %d, want 12000", budget)
	}
	controls := store.resolveLLMExecutionControls(project.ID)
	if controls.MaxParallelism != 6 || controls.MaxRepairAttempts != 2 || controls.PromptVersion != llmpipeline.PromptTemplateVersion {
		t.Fatalf("execution controls did not inherit the frozen profile: %+v", controls)
	}
}
