export type Tone = "default" | "good" | "warn" | "bad";

const labels: Record<string, string> = {
	intake: "Intake",
	sources_processed: "Sources ready",
	source_review: "Source review",
	analysis_review: "Needs decisions",
	ready_for_model_generation: "Ready for modeling",
	conceptual_review: "Conceptual review",
	model_generated: "Model generated",
	semantic_review: "Semantic review",
	semantic_repair_required: "Semantic repair",
	ready_for_dbml: "Model accepted",
	completed: "Completed",
	not_generated: "Not generated",
	needs_attention: "Needs attention",
	represented: "In model",
	intentionally_not_in_db: "Not in DB",
	requires_app_logic: "App logic",
	external_system: "External",
	unsupported: "Unsupported",
	deferred: "Deferred",
	direct_db: "Direct DB",
	model_supporting: "Supporting",
	application_logic: "App logic",
	ui_only: "UI only",
	ui_behavior: "UI behavior",
	ui_requirement: "UI requirement",
	non_model: "Non-model",
	model_relevant: "Model relevant",
	legacy_not_applicable: "Not applicable",
	not_applicable: "Not applicable",
};

export function humanizeStatus(value: string) {
	if (!value) return "";
	if (labels[value]) return labels[value];
	const text = value.replace(/_/g, " ");
	return text.charAt(0).toUpperCase() + text.slice(1);
}

export function statusTone(value: string): Tone {
	if ([
		"ready", "completed", "reviewed", "complete", "accepted", "passed", "sources_processed", "sources_ready",
		"ready_for_model_generation", "model_generated", "ready_for_dbml", "represented", "direct_db", "model_relevant",
	].includes(value)) return "good";
	if (["analysis_review", "needs_attention", "open_review", "outdated", "warning", "source_review", "conceptual_review",
		"semantic_review", "running", "queued", "retrying", "proposed", "fallback", "deferred"].includes(value)) return "warn";
	if (["failed", "blocked", "error", "interrupted", "semantic_repair_required", "unsupported"].includes(value)) return "bad";
	return "default";
}
