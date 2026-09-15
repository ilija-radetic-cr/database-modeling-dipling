import { useMutation, useQuery } from "@tanstack/react-query";
import { Download, RotateCcw } from "lucide-react";
import { api } from "@/shared/api/client";
import { Button, Metric, Panel, StatusBadge } from "@/shared/components/ui";
import { useRouter } from "@/shared/lib/router";

export function CompletedPage({ projectId }: { projectId: string }) {
  const { navigate } = useRouter();
  const project = useQuery({ queryKey: ["project", projectId], queryFn: () => api.getProject(projectId) });
  const dbml = useQuery({ queryKey: ["dbml", projectId], queryFn: () => api.dbml(projectId), retry: false });
  const decisions = useQuery({ queryKey: ["review-decisions", projectId], queryFn: () => api.reviewDecisions(projectId) });
  const reopen = useMutation({
    mutationFn: () => api.reopenProject(projectId, "Continue work after review."),
    onSuccess: ({ project }) => navigate(`/projects/${project.id}/analysis/sources`),
  });

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1 className="page-title">{project.data?.project.name ?? "Project"} | Completed</h1>
          <p className="page-subtitle">Locked snapshot with final exports and traceability summary.</p>
        </div>
        <Button onClick={() => reopen.mutate()} disabled={reopen.isPending}>
          <RotateCcw size={18} />
          Reopen for Revision
        </Button>
      </div>

      <div className="grid-3">
        <Metric label="Lifecycle" value={<StatusBadge value={project.data?.project.lifecycle_status ?? "completed"} />} />
        <Metric label="Review decisions" value={project.data?.project.counts.review_decisions ?? "-"} />
        <Metric label="Quality warnings" value={project.data?.project.quality.lint_warnings ?? "-"} />
      </div>

      <div className="grid-2">
        <Panel
          title="Final DBML"
          action={
            <div className="toolbar">
              <Button onClick={() => window.open(api.exportURL(projectId, "dbml"), "_blank")}>
                <Download size={18} />
                DBML
              </Button>
              <Button onClick={() => window.open(api.exportURL(projectId, "bundle"), "_blank")}>
                <Download size={18} />
                Bundle
              </Button>
            </div>
          }
        >
          <pre className="code-view">{dbml.data?.dbml ?? "DBML export is available after completion."}</pre>
        </Panel>
        <Panel title="Review Decisions">
          <table className="data-table">
            <thead>
              <tr>
                <th>Decision</th>
                <th>Status</th>
                <th>Selected option</th>
              </tr>
            </thead>
            <tbody>
              {(decisions.data?.items ?? []).slice(0, 16).map((decision) => (
                <tr key={decision.id}>
                  <td>
                    <strong>{decision.id}</strong>
                    <div className="muted">{decision.question}</div>
                  </td>
                  <td>
                    <StatusBadge value={decision.status} />
                  </td>
                  <td>{decision.selected_option}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Panel>
      </div>
    </div>
  );
}
