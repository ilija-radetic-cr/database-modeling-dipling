import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Download } from "lucide-react";
import { api } from "@/shared/api/client";
import { Button, Panel } from "@/shared/components/ui";

// SchemaOutput shows the two deterministic renderings of the accepted DB-DSL
// model: DBML for diagrams and PostgreSQL DDL for an actual database.
export function SchemaOutput({ projectId, ready }: { projectId: string; ready: boolean }) {
  const [format, setFormat] = useState<"dbml" | "sql">("dbml");
  const dbml = useQuery({ queryKey: ["dbml", projectId], queryFn: () => api.dbml(projectId), retry: false, enabled: ready });
  const sql = useQuery({ queryKey: ["postgresql", projectId], queryFn: () => api.postgresql(projectId), retry: false, enabled: ready && format === "sql" });
  const current = format === "dbml" ? dbml : sql;
  const text = format === "dbml" ? dbml.data?.dbml : sql.data?.sql;
  return (
    <Panel
      title="Database schema"
      action={
        <Button disabled={!ready} onClick={() => window.open(api.exportURL(projectId, format), "_blank")}>
          <Download size={18} />
          {format === "dbml" ? "DBML" : "SQL"}
        </Button>
      }
    >
      <div className="segmented">
        <button className={format === "dbml" ? "active" : ""} onClick={() => setFormat("dbml")}>DBML</button>
        <button className={format === "sql" ? "active" : ""} onClick={() => setFormat("sql")}>PostgreSQL</button>
      </div>
      {!ready || current.isError ? (
        <p className="muted">The schema is available after the final model is accepted and outputs are generated.</p>
      ) : (
        <pre className="code-view">{text ?? "Loading…"}</pre>
      )}
    </Panel>
  );
}
