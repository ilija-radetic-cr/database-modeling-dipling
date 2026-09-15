import { Check, LockKeyhole } from "lucide-react";
import type { ArtifactHealth } from "@/shared/api/types";

type Step = { id: string; label: string; done: boolean; blocked?: boolean };

export function PipelineStepper({ health }: { health: ArtifactHealth }) {
  const steps: Step[] = [
    {
		id: "sources",
		label: "Sources",
		done: health.source_manifest_status === "ready" && health.combined_document_status === "ready" && health.source_fidelity_status === "ready" && health.source_units_status === "ready",
    },
    {
		id: "evidence",
		label: "Evidence",
		done: health.requirement_atoms_status === "ready" && health.design_obligations_status === "ready" && health.functional_analysis_status === "ready" && health.crud_mapping_status === "ready",
		blocked: health.source_units_status !== "ready",
    },
    {
		id: "decisions",
		label: "Decisions",
		done: health.review_candidates_status === "ready" && health.open_review_questions === 0 && health.conceptual_model_status === "ready",
		blocked: health.review_candidates_status !== "ready",
    },
	{
		id: "model_export",
		label: "Model / Export",
		done: health.model_status === "ready" && health.semantic_verification_status === "passed" && health.final_model_accepted && health.dbml_status === "ready",
		blocked: health.conceptual_model_status !== "ready" || health.open_review_questions > 0,
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
