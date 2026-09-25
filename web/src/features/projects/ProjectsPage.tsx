import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, FolderOpen, Plus, Search, Trash2 } from "lucide-react";
import { api, projectDefaultPath } from "@/shared/api/client";
import type { ProjectSummary } from "@/shared/api/types";
import { Badge, Button, LoadingState, Metric, Panel, StatusBadge } from "@/shared/components/ui";
import { useRouter } from "@/shared/lib/router";

export function ProjectsPage() {
  const { navigate } = useRouter();
  const queryClient = useQueryClient();
  const [status, setStatus] = useState("active");
  const [search, setSearch] = useState("");
  const projects = useQuery({
    queryKey: ["projects", status, search],
    queryFn: () => api.listProjects(status, search),
  });
  // Metrics describe the whole workspace, independent of the table filter.
  const allProjects = useQuery({ queryKey: ["projects", "all", ""], queryFn: () => api.listProjects("all", "") });
  const activeProjects = useQuery({ queryKey: ["projects", "active", ""], queryFn: () => api.listProjects("active", "") });
  const deleteProject = useMutation({
    mutationFn: (projectId: string) => api.deleteProject(projectId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["projects"] }),
  });

  const confirmDelete = (project: ProjectSummary) => {
    const confirmed = window.confirm(
      `Delete project "${project.name}"?\n\nThis permanently removes the project and its workbench artifacts. Imported source bundles in poc/ are kept.`,
    );
    if (confirmed) deleteProject.mutate(project.id);
  };

  const items = projects.data?.items ?? [];
  const everything = allProjects.data?.items ?? [];
  const active = activeProjects.data?.items.length ?? 0;
  const completed = everything.filter((project) => project.lifecycle_status === "completed").length;
  // Projects stopped at a human gate: source review, conceptual review or final-model review.
  const awaitingReview = everything.filter((project) => ["source_review", "conceptual_review", "model_generated"].includes(project.lifecycle_status)).length;

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1 className="page-title">Projects</h1>
          <p className="page-subtitle">Database modeling projects from task sources, conceptual models and DSL artifacts.</p>
        </div>
        <Button variant="primary" onClick={() => navigate("/projects/new")}>
          <Plus size={18} />
          New Project
        </Button>
      </div>

      <div className="grid-3">
        <Metric label="Active projects" value={active} />
        <Metric label="Completed" value={completed} />
        <Metric label="Awaiting your review" value={awaitingReview} />
      </div>

      <Panel
        title={`${items.length} project${items.length === 1 ? "" : "s"}`}
        action={
          <div className="toolbar nowrap">
            <select className="select compact" value={status} onChange={(event) => setStatus(event.target.value)}>
              <option value="active">Active</option>
              <option value="completed">Completed</option>
              <option value="all">All</option>
            </select>
            <div style={{ position: "relative" }}>
              <Search size={16} style={{ left: 10, position: "absolute", top: 11, color: "#66758b" }} />
              <input
                className="input"
                style={{ paddingLeft: 32, width: 240 }}
                placeholder="Search projects"
                value={search}
                onChange={(event) => setSearch(event.target.value)}
              />
            </div>
          </div>
        }
      >
        {deleteProject.isError && <p className="error-text">{(deleteProject.error as Error).message}</p>}
        {projects.isLoading ? (
          <LoadingState />
        ) : (
          <ProjectsTable
            projects={items}
            onOpen={(project) => navigate(projectDefaultPath(project))}
            onDelete={confirmDelete}
            deletingId={deleteProject.isPending ? deleteProject.variables : undefined}
          />
        )}
      </Panel>
    </div>
  );
}

function ProjectsTable({
  projects,
  onOpen,
  onDelete,
  deletingId,
}: {
  projects: ProjectSummary[];
  onOpen: (project: ProjectSummary) => void;
  onDelete: (project: ProjectSummary) => void;
  deletingId?: string;
}) {
  if (projects.length === 0) {
    return <p className="muted">No projects match the current filter.</p>;
  }
  return (
    <table className="data-table projects-table">
      <thead>
        <tr>
          <th>Project</th>
          <th>Status</th>
          <th>Analysis</th>
          <th>Quality</th>
          <th aria-label="Actions" />
        </tr>
      </thead>
      <tbody>
        {projects.map((project) => (
          <tr key={project.id} className="clickable-row" onClick={() => onOpen(project)}>
            <td>
              <strong>{project.name}</strong>
              <div className="muted small truncate" title={project.last_activity}>{project.last_activity || project.description}</div>
            </td>
            <td>
              <StatusBadge value={project.lifecycle_status} />
            </td>
            <td className="muted">
              {project.counts.source_units} source units
              {project.counts.entities !== undefined && ` · ${project.counts.entities} entities`}
            </td>
            <td>
              {project.quality.validation_errors > 0 ? (
                <Badge tone="bad">{project.quality.validation_errors} errors</Badge>
              ) : (
                <Badge tone="good">valid</Badge>
              )}
              {project.quality.lint_warnings > 0 && <span className="muted small"> · {project.quality.lint_warnings} warnings</span>}
            </td>
            <td onClick={(event) => event.stopPropagation()}>
              <div className="toolbar row-actions">
                <Button onClick={() => onOpen(project)}>
                  <FolderOpen size={16} />
                  Open
                </Button>
                {project.lifecycle_status === "completed" && (
                  <Button onClick={() => window.open(api.exportURL(project.id, "dbml"), "_blank")}>
                    <Download size={16} />
                    DBML
                  </Button>
                )}
                <Button
                  variant="ghost"
                  className="icon-danger"
                  disabled={deletingId === project.id}
                  onClick={() => onDelete(project)}
                  aria-label={`Delete ${project.name}`}
                  title="Delete project"
                >
                  <Trash2 size={16} />
                </Button>
              </div>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
