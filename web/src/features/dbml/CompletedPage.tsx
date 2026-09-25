import { useEffect } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Download, RotateCcw } from "lucide-react";
import { api } from "@/shared/api/client";
import { Button, Metric, StatusBadge } from "@/shared/components/ui";
import { useRouter } from "@/shared/lib/router";
import { SchemaOutput } from "./SchemaOutput";

export function CompletedPage({ projectId }: { projectId: string }) {
  const { navigate, redirect } = useRouter();
  const project = useQuery({ queryKey: ["project", projectId], queryFn: () => api.getProject(projectId) });
  const lifecycle = project.data?.project.lifecycle_status;
  // Only completed projects have a locked snapshot; others finalize on the DBML page.
  useEffect(() => {
    if (lifecycle && lifecycle !== "completed") redirect(`/projects/${projectId}/dbml`);
  }, [lifecycle, redirect, projectId]);
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
        <div className="toolbar">
          <Button onClick={() => window.open(api.exportURL(projectId, "report"), "_blank")}>
            <Download size={18} />
            Trace report
          </Button>
          <Button onClick={() => window.open(api.exportURL(projectId, "bundle"), "_blank")}>
            <Download size={18} />
            Full bundle
          </Button>
          <Button onClick={() => reopen.mutate()} disabled={reopen.isPending}>
            <RotateCcw size={18} />
            Reopen for Revision
          </Button>
        </div>
      </div>

      <div className="grid-3">
        <Metric label="Lifecycle" value={<StatusBadge value={project.data?.project.lifecycle_status ?? "completed"} />} />
        <Metric label="Entities" value={project.data?.project.counts.entities ?? "-"} />
        <Metric label="Quality warnings" value={project.data?.project.quality.lint_warnings ?? "-"} />
      </div>

      <SchemaOutput projectId={projectId} ready />
    </div>
  );
}
