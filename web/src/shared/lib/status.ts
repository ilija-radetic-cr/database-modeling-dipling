export type Tone = "default" | "good" | "warn" | "bad";

const labels: Record<string, string> = {
	intake: "Intake",
	sources_processed: "Sources ready",
	source_review: "Source review",
	conceptual_review: "Conceptual review",
	ready_for_model_generation: "Ready for modeling",
	model_generated: "Model generated",
	ready_for_dbml: "Model accepted",
	completed: "Completed",
	not_generated: "Not generated",
	needs_attention: "Needs attention",
	// Source-unit relevance values.
	model_relevant: "Model relevant",
	model_supporting: "Supporting",
	non_model: "Non-model",
};

export function humanizeStatus(value: string) {
	if (!value) return "";
	if (labels[value]) return labels[value];
	const text = value.replace(/_/g, " ");
	return text.charAt(0).toUpperCase() + text.slice(1);
}

export function statusTone(value: string): Tone {
	if ([
		"ready", "completed", "reviewed", "complete", "accepted", "sources_processed",
		"ready_for_model_generation", "model_generated", "ready_for_dbml", "model_relevant",
	].includes(value)) return "good";
	if (["needs_attention", "outdated", "warning", "source_review", "conceptual_review",
		"running", "queued", "retrying", "proposed"].includes(value)) return "warn";
	if (["failed", "blocked", "error", "interrupted"].includes(value)) return "bad";
	return "default";
}
