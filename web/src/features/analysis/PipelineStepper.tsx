import { Check, LockKeyhole } from "lucide-react";
import type { ArtifactHealth } from "@/shared/api/types";

type Step = { id: string; label: string; done: boolean; blocked?: boolean };

// The segment-based flow: segmentation and segment IDs, the conceptual model
// (the only other LLM step), then the deterministic model and its outputs.
export function PipelineStepper({ health }: { health: ArtifactHealth }) {
	const steps: Step[] = [
		{
			id: "sources",
			label: "Segments",
			done: health.source_manifest_status === "ready" && health.combined_document_status === "ready" && health.source_segmentation_status === "ready" && health.source_units_status === "ready",
		},
		{
			id: "conceptual",
			label: "Conceptual Model",
			done: health.conceptual_model_status === "ready",
			blocked: health.source_units_status !== "ready",
		},
		{
			id: "model",
			label: "Logical Model",
			done: health.model_status === "ready" && health.final_model_accepted,
			blocked: health.conceptual_model_status !== "ready",
		},
		{
			id: "export",
			label: "DBML / Export",
			done: health.dbml_status === "ready",
			blocked: !health.final_model_accepted,
		},
	];
	const current = Math.max(0, steps.findIndex((step) => !step.done));

	return (
		<ol className="pipeline-stepper" aria-label="Project pipeline">
			{steps.map((step, index) => {
				const state = step.done ? "done" : index === current ? "current" : "pending";
				return (
					<li className={`pipeline-step ${state}`} key={step.id} aria-current={state === "current" ? "step" : undefined}>
						<span className="pipeline-step-icon" aria-hidden="true">
							{step.done ? <Check size={14} /> : step.blocked ? <LockKeyhole size={13} /> : index + 1}
						</span>
						<span>{step.label}</span>
					</li>
				);
			})}
		</ol>
	);
}
