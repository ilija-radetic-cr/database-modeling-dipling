import { useEffect, useMemo } from "react";
import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { BarChart3, CheckCircle2, FolderKanban, Settings, Workflow } from "lucide-react";
import { api, projectDefaultPath } from "@/shared/api/client";
import { routeParts, useRouter } from "@/shared/lib/router";
import { finalizePath } from "@/shared/lib/autopilot";

const lastProjectStorageKey = "dbdsl:lastProjectId";

function readLastProject() {
  try {
    return window.localStorage.getItem(lastProjectStorageKey);
  } catch {
    return null;
  }
}

export function AppShell({ children }: { children: ReactNode }) {
  return (
    <div className="app-shell">
      <Sidebar />
      <main className="main">{children}</main>
    </div>
  );
}

function Sidebar() {
  const { path, navigate } = useRouter();
  const routeProjectId = projectIdFromPath(path);
  const projects = useQuery({
    queryKey: ["projects", "all", ""],
    queryFn: () => api.listProjects("all", ""),
    staleTime: 5000,
  });

  useEffect(() => {
    if (routeProjectId) {
      try {
        window.localStorage.setItem(lastProjectStorageKey, routeProjectId);
      } catch {
        // Remembering the last project is a convenience only.
      }
    }
  }, [routeProjectId]);

  const projectItems = projects.data?.items ?? [];
  // A remembered project may have been deleted; fall back to the most recent
  // open project, then to any project. With no projects there is no current one.
  const rememberedProjectId = readLastProject();
  const currentProjectId: string | undefined = routeProjectId
    ?? (rememberedProjectId && (projects.isLoading || projectItems.some((project) => project.id === rememberedProjectId)) ? rememberedProjectId : undefined)
    ?? projectItems.find((project) => project.lifecycle_status !== "completed")?.id
    ?? projectItems[0]?.id;
  const currentProject = currentProjectId ? projectItems.find((project) => project.id === currentProjectId) : undefined;
  const currentProjectHref = currentProject
    ? projectDefaultPath(currentProject)
    : currentProjectId ? `/projects/${currentProjectId}/analysis/sources` : "/projects/new";
  const modelHref = currentProjectId ? `/projects/${currentProjectId}/model/conceptual` : "/projects";
  const finalizeHref = currentProjectId ? finalizePath(currentProjectId, currentProject?.lifecycle_status) : "/projects";
	const projectState = useQuery({
		queryKey: ["project", currentProjectId],
		queryFn: () => api.getProject(currentProjectId ?? ""),
		enabled: !!currentProject,
		retry: false,
	});
	const health = projectState.data?.artifact_health;

	const items = useMemo<Array<{ label: string; icon: typeof Workflow; href: string; active: boolean; disabled?: boolean; reason?: string }>>(
    () => [
      { label: "Projects", icon: FolderKanban, href: "/projects", active: path === "/projects" || path === "/projects/new" },
      {
        label: "Current Project",
        icon: Workflow,
        href: currentProjectHref,
        active: !!currentProjectId && path.startsWith(`/projects/${currentProjectId}/`) && !path.includes("/model") && !path.includes("/completed") && !path.endsWith("/dbml"),
        disabled: !currentProjectId,
        reason: "Create or open a project first.",
      },
      {
        label: "Model",
        icon: BarChart3,
		href: modelHref,
        active: !!currentProjectId && path.startsWith(`/projects/${currentProjectId}/model`),
		// A proposed conceptual model is reviewed in the model workspace, so it opens as soon as one exists.
		disabled: (health?.conceptual_model_status ?? "not_generated") === "not_generated" && health?.model_status !== "ready",
		reason: "The model workspace unlocks after the conceptual model stage.",
      },
      {
        label: currentProject?.lifecycle_status === "completed" ? "Completed" : "Finalize",
        icon: CheckCircle2,
        href: finalizeHref,
        active: !!currentProjectId && (path.startsWith(`/projects/${currentProjectId}/completed`) || path.startsWith(`/projects/${currentProjectId}/dbml`)),
		disabled: health?.dbml_status !== "ready" && !health?.can_complete_project && currentProject?.lifecycle_status !== "completed",
		reason: "Finalize the accepted model and generate current DBML and trace outputs first.",
      },
      { label: "Settings", icon: Settings, href: "/settings", active: path === "/settings" },
    ],
	[currentProject?.lifecycle_status, currentProjectHref, currentProjectId, finalizeHref, health, modelHref, path],
  );

  function switchProject(projectId: string) {
    const project = projectItems.find((item) => item.id === projectId);
    window.localStorage.setItem(lastProjectStorageKey, projectId);
    navigate(project ? projectDefaultPath(project) : `/projects/${projectId}/analysis/sources`);
  }

  return (
    <aside className="sidebar">
      <div className="brand">
        <span className="brand-mark">DB</span>
        <span>Model Workbench</span>
      </div>
      <label className="project-switcher">
        <span>Current project</span>
        <select className="select" value={currentProjectId ?? ""} onChange={(event) => switchProject(event.target.value)}>
          {!currentProject && <option value={currentProjectId ?? ""}>{currentProjectId ?? "No projects yet"}</option>}
          {projectItems.map((project) => (
            <option value={project.id} key={project.id}>
              {project.name}
            </option>
          ))}
        </select>
      </label>
      <nav className="nav-list">
        {items.map((item) => {
          const Icon = item.icon;
          return (
			<button className={`nav-item ${item.active ? "active" : ""}`} key={item.label} onClick={() => navigate(item.href)} disabled={item.disabled} title={item.disabled ? item.reason : undefined}>
              <Icon size={18} />
              <span>{item.label}</span>
            </button>
          );
        })}
      </nav>
    </aside>
  );
}

function projectIdFromPath(path: string) {
  const parts = routeParts(path);
  if (parts[0] !== "projects" || !parts[1] || parts[1] === "new") {
    return null;
  }
  return parts[1];
}
