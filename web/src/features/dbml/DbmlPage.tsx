import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Download, Lock, RefreshCcw } from "lucide-react";
import { api } from "@/shared/api/client";
import { Button, Metric, Panel, StatusBadge } from "@/shared/components/ui";
import { useRouter } from "@/shared/lib/router";

export function DbmlPage({ projectId }: { projectId: string }) {
  const { navigate } = useRouter();
  const queryClient = useQueryClient();
  const project = useQuery({ queryKey: ["project", projectId], queryFn: () => api.getProject(projectId) });
  const dbml = useQuery({ queryKey: ["dbml", projectId], queryFn: () => api.dbml(projectId), retry: false });
  const regenerate = useMutation({
    mutationFn: () => api.regenerateDbml(projectId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["dbml", projectId] }),
  });
  const complete = useMutation({
    mutationFn: () => api.completeProject(projectId, project.data?.project.current_revision ?? 0),
    onSuccess: ({ project }) => navigate(`/projects/${project.id}/completed`),
  });

  const health = project.data?.artifact_health;
  const canComplete = health?.can_complete_project ?? false;

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1 className="page-title">{project.data?.project.name ?? "Project"} | DBML Finalization</h1>
          <p className="page-subtitle">Read-only DBML output, exports and completion gate.</p>
        </div>
        <div className="toolbar">
          <Button onClick={() => regenerate.mutate()} disabled={regenerate.isPending}>
            <RefreshCcw size={18} />
            Regenerate
          </Button>
          <Button variant="primary" disabled={!canComplete || complete.isPending} onClick={() => complete.mutate()}>
            <Lock size={18} />
            Complete Project
          </Button>
        </div>
      </div>

      <div className="grid-3">
        <Metric label="Validation errors" value={project.data?.project.quality.validation_errors ?? "-"} />
        <Metric label="Open reviews" value={project.data?.project.counts.open_review_questions ?? "-"} />
        <Metric label="DBML status" value={<StatusBadge value={health?.dbml_status ?? "not_generated"} />} />
      </div>

      <div className="grid-2">
        <Panel title="DBML">
          {dbml.isError ? (
            <p className="muted">DBML is not ready yet. Generate the database model first.</p>
          ) : (
            <pre className="code-view">{dbml.data?.dbml ?? ""}</pre>
          )}
        </Panel>
        <Panel title="Completion Checklist">
          <div className="field" style={{ gap: 12 }}>
            <ChecklistItem done={(project.data?.project.quality.validation_errors ?? 1) === 0} label="Validation errors = 0" />
            <ChecklistItem done={(project.data?.project.counts.open_review_questions ?? 1) === 0} label="Open review questions = 0" />
            <ChecklistItem done={health?.can_continue_to_dbml ?? false} label="Model generated from current analysis" />
            <ChecklistItem done={health?.dbml_status === "ready"} label="DBML ready" />
            <div className="toolbar">
              <Button disabled={dbml.isError} onClick={() => window.open(api.exportURL(projectId, "dbml"), "_blank")}>
                <Download size={18} />
                DBML
              </Button>
              <Button disabled={dbml.isError} onClick={() => window.open(api.exportURL(projectId, "report"), "_blank")}>
                <Download size={18} />
                Report
              </Button>
              <Button disabled={dbml.isError} onClick={() => window.open(api.exportURL(projectId, "bundle"), "_blank")}>
                <Download size={18} />
                Bundle
              </Button>
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
