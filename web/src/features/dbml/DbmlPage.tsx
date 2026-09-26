import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Lock, RefreshCcw } from "lucide-react";
import { api } from "@/shared/api/client";
import type { Job } from "@/shared/api/types";
import { Button, Metric, Panel, StatusBadge } from "@/shared/components/ui";
import { JobProgress } from "@/features/jobs/JobProgress";
import { ExportButton } from "@/shared/components/ExportButton";
import { useRouter } from "@/shared/lib/router";
import { SchemaOutput } from "./SchemaOutput";

export function DbmlPage({ projectId }: { projectId: string }) {
  const { navigate, redirect } = useRouter();
  const queryClient = useQueryClient();
  const project = useQuery({ queryKey: ["project", projectId], queryFn: () => api.getProject(projectId) });
  const dbml = useQuery({ queryKey: ["dbml", projectId], queryFn: () => api.dbml(projectId), retry: false });
  const [regenerateJob, setRegenerateJob] = useState<Job | null>(null);
  const regenerate = useMutation({
    mutationFn: () => api.regenerateDbml(projectId),
    onSuccess: ({ job }) => setRegenerateJob(job),
  });
  const complete = useMutation({
    mutationFn: () => api.completeProject(projectId, project.data?.project.current_revision ?? 0),
    onSuccess: async ({ project: completedProject }) => {
      // CompletedPage redirects non-completed projects. Refresh the shared
      // project cache before navigating so it cannot see the fresh pre-complete
      // value and bounce the user back to this page.
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["project", completedProject.id] }),
        queryClient.invalidateQueries({ queryKey: ["projects"] }),
      ]);
      navigate(`/projects/${completedProject.id}/completed`);
    },
  });

  const health = project.data?.artifact_health;
  const canComplete = health?.can_complete_project ?? false;
  const lifecycle = project.data?.project.lifecycle_status;
  // A completed project is locked; its outputs live on the completed page.
  useEffect(() => {
    if (lifecycle === "completed") redirect(`/projects/${projectId}/completed`);
  }, [lifecycle, redirect, projectId]);

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1 className="page-title">{project.data?.project.name ?? "Project"} | Finalization</h1>
          <p className="page-subtitle">DBML and PostgreSQL schema, exports and completion gate.</p>
        </div>
        <div className="toolbar">
          <Button onClick={() => regenerate.mutate()} disabled={regenerate.isPending || !!regenerateJob || complete.isPending}>
            <RefreshCcw size={18} />
            Regenerate
          </Button>
          <Button variant="primary" disabled={!canComplete || complete.isPending || !!regenerateJob} onClick={() => complete.mutate()}>
            <Lock size={18} />
            Complete Project
          </Button>
        </div>
      </div>

      {regenerateJob && (
        <Panel title="Regenerating final outputs">
          <JobProgress
            projectId={projectId}
            job={regenerateJob}
            onDone={() => {
              setRegenerateJob(null);
              void Promise.all([
                queryClient.invalidateQueries({ queryKey: ["project", projectId] }),
                queryClient.invalidateQueries({ queryKey: ["dbml", projectId] }),
                queryClient.invalidateQueries({ queryKey: ["postgresql", projectId] }),
              ]);
            }}
            onDismiss={() => setRegenerateJob(null)}
          />
        </Panel>
      )}
      {(regenerate.isError || complete.isError) && (
        <p className="error-text">
          {(regenerate.error ?? complete.error) instanceof Error
            ? (regenerate.error ?? complete.error)?.message
            : "The finalization action failed."}
        </p>
      )}
      {regenerateJob && <p className="muted">Downloads and completion are paused until the refreshed outputs are ready.</p>}

      <div className="grid-3">
        <Metric label="Validation errors" value={project.data?.project.quality.validation_errors ?? "-"} />
        <Metric label="Final model" value={<StatusBadge value={health?.final_model_accepted ? "accepted" : "not_accepted"} />} />
        <Metric label="DBML status" value={<StatusBadge value={health?.dbml_status ?? "not_generated"} />} />
      </div>

      <div className="grid-2">
        <SchemaOutput projectId={projectId} ready={!regenerateJob && !dbml.isError && health?.dbml_status === "ready"} />
        <Panel title="Completion Checklist">
          <div className="field" style={{ gap: 12 }}>
            <ChecklistItem done={(project.data?.project.quality.validation_errors ?? 1) === 0} label="Validation errors = 0" />
            <ChecklistItem done={health?.final_model_accepted ?? false} label="Final model accepted" />
            <ChecklistItem done={health?.can_continue_to_dbml ?? false} label="Model generated from current analysis" />
            <ChecklistItem done={health?.dbml_status === "ready"} label="DBML ready" />
            <div className="toolbar">
              <ExportButton projectId={projectId} kind="dbml" disabled={dbml.isError || !!regenerateJob}>DBML</ExportButton>
              <ExportButton projectId={projectId} kind="sql" disabled={dbml.isError || !!regenerateJob}>SQL</ExportButton>
              <ExportButton projectId={projectId} kind="report" disabled={dbml.isError || !!regenerateJob}>Report</ExportButton>
              <ExportButton projectId={projectId} kind="bundle" disabled={dbml.isError || !!regenerateJob}>Bundle</ExportButton>
            </div>
          </div>
        </Panel>
      </div>
    </div>
  );
}

function ChecklistItem({ done, label }: { done: boolean; label: string }) {
  return (
    <div className="toolbar">
      <CheckCircle2 size={18} color={done ? "#0f766e" : "#94a3b8"} />
      <span>{label}</span>
    </div>
  );
}
