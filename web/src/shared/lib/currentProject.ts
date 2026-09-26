import type { ProjectSummary } from "@/shared/api/types";

export function selectCurrentProjectId(
  projects: ProjectSummary[],
  routeProjectId: string | null,
  rememberedProjectId: string | null,
  loading: boolean,
) {
  if (loading) return routeProjectId ?? undefined;
  const exists = (id: string | null) => !!id && projects.some((project) => project.id === id);
  if (exists(routeProjectId)) return routeProjectId!;
  if (exists(rememberedProjectId)) return rememberedProjectId!;
  return projects.find((project) => project.lifecycle_status !== "completed")?.id ?? projects[0]?.id;
}
