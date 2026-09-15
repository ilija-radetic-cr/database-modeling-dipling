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
  const active = items.filter((project) => project.lifecycle_status !== "completed").length;
  const completed = items.filter((project) => project.lifecycle_status === "completed").length;

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1 className="page-title">Projects</h1>
          <p className="page-subtitle">Database modeling projects from task sources, review decisions and DSL artifacts.</p>
        </div>
        <Button variant="primary" onClick={() => navigate("/projects/new")}>
          <Plus size={18} />
          New Project
        </Button>
      </div>

      <div className="grid-3">
        <Metric label="Active projects" value={active} />
        <Metric label="Completed" value={completed} />
        <Metric label="Open reviews" value={items.reduce((sum, item) => sum + item.counts.open_review_questions, 0)} />
      </div>

      <Panel
        title="Workspace"
        action={
          <div className="toolbar">
            <select className="select" value={status} onChange={(event) => setStatus(event.target.value)}>
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
    <table className="data-table">
      <thead>
        <tr>
          <th style={{ width: "25%" }}>Project</th>
          <th>Status</th>
          <th>Analysis</th>
          <th>Quality</th>
          <th>Last activity</th>
          <th style={{ width: 280 }}>Actions</th>
        </tr>
      </thead>
      <tbody>
        {projects.map((project) => (
          <tr key={project.id}>
            <td>
              <strong>{project.name}</strong>
              <div className="muted truncate">{project.description}</div>
            </td>
            <td>
              <StatusBadge value={project.lifecycle_status} />
            </td>
            <td>
              <div className="toolbar">
                <Badge>{project.counts.source_units} sources</Badge>
                <Badge>{project.counts.requirements} reqs</Badge>
                <Badge tone={project.counts.open_review_questions ? "warn" : "good"}>{project.counts.open_review_questions} reviews</Badge>
              </div>
            </td>
            <td>
              <div className="toolbar">
                <Badge tone={project.quality.validation_errors ? "bad" : "good"}>{project.quality.validation_errors} errors</Badge>
                <Badge tone={project.quality.lint_warnings ? "warn" : "good"}>{project.quality.lint_warnings} warnings</Badge>
              </div>
            </td>
            <td className="muted">{project.last_activity}</td>
            <td>
              <div className="toolbar">
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
                  variant="danger"
                  disabled={deletingId === project.id}
                  onClick={() => onDelete(project)}
                  aria-label={`Delete ${project.name}`}
                >
                  <Trash2 size={16} />
                  {deletingId === project.id ? "Deleting..." : "Delete"}
                </Button>
              </div>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
