import { useEffect } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { RotateCcw } from "lucide-react";
import { api } from "@/shared/api/client";
import { Button, Metric, StatusBadge } from "@/shared/components/ui";
import { useRouter } from "@/shared/lib/router";
import { SchemaOutput } from "./SchemaOutput";
import { ExportButton } from "@/shared/components/ExportButton";

export function CompletedPage({ projectId }: { projectId: string }) {
  const { navigate, redirect } = useRouter();
  const queryClient = useQueryClient();
  const project = useQuery({ queryKey: ["project", projectId], queryFn: () => api.getProject(projectId) });
  const lifecycle = project.data?.project.lifecycle_status;
  // Only completed projects have a locked snapshot; others finalize on the DBML page.
  useEffect(() => {
    if (lifecycle && lifecycle !== "completed") redirect(`/projects/${projectId}/dbml`);
  }, [lifecycle, redirect, projectId]);
  const reopen = useMutation({
    mutationFn: () => api.reopenProject(projectId, "Continue work after review."),
    onSuccess: async ({ project: reopenedProject }) => {
      // Reopen creates a new revision project. Prime that project and refresh
      // the switcher before changing routes so navigation never points at a
      // project the sidebar still considers unknown.
      await Promise.all([
        queryClient.fetchQuery({
          queryKey: ["project", reopenedProject.id],
          queryFn: () => api.getProject(reopenedProject.id),
          staleTime: 0,
        }),
        queryClient.invalidateQueries({ queryKey: ["projects"] }),
      ]);
      navigate(`/projects/${reopenedProject.id}/analysis/sources`);
    },
  });

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1 className="page-title">{project.data?.project.name ?? "Project"} | Completed</h1>
          <p className="page-subtitle">Locked snapshot with final exports and traceability summary.</p>
        </div>
        <div className="toolbar">
          <ExportButton projectId={projectId} kind="report">Trace report</ExportButton>
          <ExportButton projectId={projectId} kind="bundle">Full bundle</ExportButton>
          <Button onClick={() => reopen.mutate()} disabled={reopen.isPending}>
            <RotateCcw size={18} />
            Reopen for Revision
          </Button>
        </div>
      </div>

      {reopen.isError && (
        <p className="error-text">{reopen.error instanceof Error ? reopen.error.message : "The project could not be reopened."}</p>
      )}

      <div className="grid-3">
        <Metric label="Lifecycle" value={<StatusBadge value={project.data?.project.lifecycle_status ?? "completed"} />} />
        <Metric label="Entities" value={project.data?.project.counts.entities ?? "-"} />
        <Metric label="Quality warnings" value={project.data?.project.quality.lint_warnings ?? "-"} />
      </div>

      <SchemaOutput projectId={projectId} ready />
    </div>
  );
}
