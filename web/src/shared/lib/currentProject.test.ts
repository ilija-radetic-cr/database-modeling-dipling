import { describe, expect, it } from "vitest";
import type { ProjectSummary } from "@/shared/api/types";
import { selectCurrentProjectId } from "./currentProject";

const project = (id: string, lifecycle_status: ProjectSummary["lifecycle_status"] = "intake") => ({
  id,
  name: id,
  lifecycle_status,
  current_revision: 1,
  counts: { resources: 0, source_units: 0 },
  quality: { validation_errors: 0, lint_warnings: 0, traceability_status: "not_generated", dbml_status: "not_generated" },
  created_at: "2026-09-25T00:00:00Z",
  updated_at: "2026-09-25T00:00:00Z",
}) as ProjectSummary;

describe("current project selection", () => {
  it("drops project ids remembered from a different empty backend", () => {
    expect(selectCurrentProjectId([], null, "project_003", false)).toBeUndefined();
    expect(selectCurrentProjectId([], "project_003", "project_003", false)).toBeUndefined();
  });

  it("prefers a listed route, then a listed remembered project, then an active project", () => {
    const projects = [project("completed", "completed"), project("active")];
    expect(selectCurrentProjectId(projects, "completed", "active", false)).toBe("completed");
    expect(selectCurrentProjectId(projects, null, "completed", false)).toBe("completed");
    expect(selectCurrentProjectId(projects, null, "missing", false)).toBe("active");
  });
});
