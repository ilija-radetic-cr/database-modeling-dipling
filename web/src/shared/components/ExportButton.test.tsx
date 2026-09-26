import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { ExportButton } from "./ExportButton";

describe("ExportButton", () => {
  it("uses the embedded browser's proven download context for real exports", () => {
    const markup = renderToStaticMarkup(<ExportButton projectId="project_007" kind="bundle">Bundle</ExportButton>);

    expect(markup).toContain("<button");
    expect(markup).not.toContain("href=");
    expect(markup).not.toContain("blob:");
  });

  it("removes navigation from a disabled export", () => {
    const markup = renderToStaticMarkup(<ExportButton projectId="project_007" kind="dbml" disabled />);

    expect(markup).toContain('disabled=""');
    expect(markup).not.toContain("href=");
  });
});
