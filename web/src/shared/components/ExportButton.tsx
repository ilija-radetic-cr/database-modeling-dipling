import type { ReactNode } from "react";
import { Download } from "lucide-react";
import { api } from "@/shared/api/client";
import { Button } from "./ui";

type ExportKind = "dbml" | "sql" | "report" | "bundle";

const extension: Record<ExportKind, string> = {
  dbml: "dbml",
  sql: "sql",
  report: "md",
  bundle: "zip",
};

export function ExportButton({
  projectId,
  kind,
  disabled,
  children,
  iconSize = 18,
}: {
  projectId: string;
  kind: ExportKind;
  disabled?: boolean;
  children?: ReactNode;
  iconSize?: number;
}) {
  const href = api.exportURL(projectId, kind);
  const label = children ?? kind.toUpperCase();

  if (!href.startsWith("data:")) {
    return (
      <div className="export-action">
        <Button disabled={disabled} onClick={() => window.open(href, "_blank")}>
          <Download size={iconSize} />
          {label}
        </Button>
      </div>
    );
  }

  return (
    <div className="export-action">
      <a
        className="button"
        href={disabled ? undefined : href}
        download={`${projectId}.${extension[kind]}`}
        aria-disabled={disabled || undefined}
        tabIndex={disabled ? -1 : undefined}
        onClick={(event) => {
          if (disabled) event.preventDefault();
        }}
      >
        <Download size={iconSize} />
        {label}
      </a>
    </div>
  );
}
