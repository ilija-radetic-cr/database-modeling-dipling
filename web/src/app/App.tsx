import { AppShell } from "./layout/AppShell";
import { ProjectsPage } from "@/features/projects/ProjectsPage";
import { NewProjectPage } from "@/features/intake/NewProjectPage";
import { AnalysisPage } from "@/features/analysis/AnalysisPage";
import { ModelPage } from "@/features/model/ModelPage";
import { DbmlPage } from "@/features/dbml/DbmlPage";
import { CompletedPage } from "@/features/dbml/CompletedPage";
import { Button, Panel } from "@/shared/components/ui";
import { routeParts, useRouter } from "@/shared/lib/router";

export function App() {
  const { path, navigate } = useRouter();
  const parts = routeParts(path);
  let page = <ProjectsPage />;

  if (parts[0] === "projects" && parts[1] === "new") {
    page = <NewProjectPage />;
  } else if (parts[0] === "projects" && parts[1] && parts[2] === "analysis") {
		page = <AnalysisPage projectId={parts[1]} mode={parts[3] ?? "overview"} />;
  } else if (parts[0] === "projects" && parts[1] && parts[2] === "model") {
    page = <ModelPage projectId={parts[1]} mode={parts[3] ?? "trace"} />;
  } else if (parts[0] === "projects" && parts[1] && parts[2] === "dbml") {
    page = <DbmlPage projectId={parts[1]} />;
  } else if (parts[0] === "projects" && parts[1] && parts[2] === "completed") {
    page = <CompletedPage projectId={parts[1]} />;
  } else if (parts[0] === "settings") {
    page = (
      <div className="page">
        <div className="page-header">
          <div>
            <h1 className="page-title">Settings</h1>
            <p className="page-subtitle">Local development settings are controlled with Vite environment variables.</p>
          </div>
          <Button onClick={() => navigate("/projects")}>Back</Button>
        </div>
        <Panel title="API mode">
          <p className="muted">Use VITE_API_MODE=mock for fixture mode or run the Go API server for real mode.</p>
        </Panel>
      </div>
    );
  }

  return <AppShell>{page}</AppShell>;
}
