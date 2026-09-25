import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, CheckCircle2, Database, Eye, FileUp, FolderOpen, Play, Sparkles, Trash2, UserCheck } from "lucide-react";
import { api } from "@/shared/api/client";
import type { BundleCandidate, InputResource, Job } from "@/shared/api/types";
import { Badge, Button, Drawer, Field, LoadingState, Panel, StatusBadge } from "@/shared/components/ui";
import { useRouter } from "@/shared/lib/router";
import { JobProgress } from "@/features/jobs/JobProgress";
import { requestAutoRun } from "@/shared/lib/autopilot";

export function NewProjectPage() {
  const { navigate } = useRouter();
  const queryClient = useQueryClient();
  const [projectId, setProjectId] = useState<string | null>(null);
  const [projectRevision, setProjectRevision] = useState(1);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [sourceTitle] = useState("Main task text");
	const [sourceText, setSourceText] = useState("");
	const [bundlePath, setBundlePath] = useState("");
  const [llmModel, setLlmModel] = useState("gpt-5.6-sol");
  const [llmUseMock, setLlmUseMock] = useState(false);
  const [job, setJob] = useState<Job | null>(null);
  const [selectedFiles, setSelectedFiles] = useState<File[]>([]);
  const [selectedResource, setSelectedResource] = useState<InputResource | null>(null);
  const [textAdded, setTextAdded] = useState(false);
  const [phase, setPhase] = useState("");

  const llmStatus = useQuery({
    queryKey: ["llm-status"],
    queryFn: () => api.llmStatus(),
  });
  const bundles = useQuery({
    queryKey: ["bundles"],
    queryFn: () => api.listBundles(),
  });
  const project = useQuery({
    queryKey: ["project", projectId],
    queryFn: () => api.getProject(projectId ?? ""),
    enabled: projectId !== null,
  });
  const resources = useQuery({
    queryKey: ["resources", projectId],
    queryFn: () => api.listResources(projectId ?? ""),
    enabled: projectId !== null,
  });
  const sourceManifest = useQuery({
    queryKey: ["source-manifest", projectId],
    queryFn: () => api.sourceManifest(projectId ?? ""),
    enabled: projectId !== null,
  });
  const resourcePreview = useQuery({
    queryKey: ["resource-text", projectId, selectedResource?.id],
    queryFn: () => api.resourceText(projectId ?? "", selectedResource?.id ?? ""),
    enabled: projectId !== null && selectedResource !== null,
  });
  // One action from task text to a running pipeline: create the project, attach
  // every source, build the lossless source document, then hand over to autopilot.
  const start = useMutation({
    mutationFn: async () => {
      let id = projectId;
      if (!id) {
        setPhase("Creating project");
        const created = await api.createProject({ name, description, language: "sr-Cyrl", domain: "information_system" });
        id = created.project.id;
        setProjectId(id);
      }
      if (sourceText.trim() !== "" && !textAdded) {
        setPhase("Adding task text");
        await api.addPastedText(id, { title: sourceTitle.trim() || "Task text", content: sourceText });
        setTextAdded(true);
      }
      for (const file of selectedFiles) {
        setPhase(`Uploading ${file.name}`);
        const form = new FormData();
        form.append("file", file);
        form.append("title", file.name);
        await api.uploadFile(id, form);
      }
      setSelectedFiles([]);
      setPhase("Building the source document");
      const fresh = await api.getProject(id);
      const started = await api.processSources(id, fresh.project.current_revision, { model: llmModel.trim() || undefined, mock: llmUseMock });
      return started.job;
    },
    onSuccess: (started) => {
      setPhase("");
      setJob(started);
      void queryClient.invalidateQueries();
    },
    onError: () => setPhase(""),
  });
  const addText = useMutation({
    mutationFn: (id: string) => api.addPastedText(id, { title: sourceTitle, content: sourceText }),
    onSuccess: ({ project_revision }) => {
      setProjectRevision(project_revision);
      void queryClient.invalidateQueries({ queryKey: ["resources"] });
      void queryClient.invalidateQueries({ queryKey: ["source-manifest"] });
      void queryClient.invalidateQueries({ queryKey: ["project"] });
    },
  });
  const upload = useMutation({
    mutationFn: async (id: string) => {
      for (const file of selectedFiles) {
        const form = new FormData();
        form.append("file", file);
        form.append("title", file.name);
        const result = await api.uploadFile(id, form);
        setProjectRevision(result.project_revision);
      }
    },
    onSuccess: () => {
      setSelectedFiles([]);
      void queryClient.invalidateQueries({ queryKey: ["resources"] });
      void queryClient.invalidateQueries({ queryKey: ["source-manifest"] });
      void queryClient.invalidateQueries({ queryKey: ["project"] });
    },
  });
  const removeResource = useMutation({
    mutationFn: (resourceId: string) => api.deleteResource(projectId ?? "", resourceId),
    onSuccess: ({ project_revision }) => {
      setProjectRevision(project_revision);
      setSelectedResource(null);
      void queryClient.invalidateQueries({ queryKey: ["resources"] });
      void queryClient.invalidateQueries({ queryKey: ["source-manifest"] });
      void queryClient.invalidateQueries({ queryKey: ["combined-document"] });
      void queryClient.invalidateQueries({ queryKey: ["project"] });
    },
  });
  const process = useMutation({
    mutationFn: (id: string) =>
		api.processSources(id, project.data?.project.current_revision ?? projectRevision, {
			model: llmModel.trim() || undefined,
			mock: llmUseMock,
		}),
    onSuccess: ({ job }) => setJob(job),
  });
  const importExisting = useMutation({
    mutationFn: (path: string) => api.importBundle(path),
    onSuccess: ({ project }) => {
      void queryClient.invalidateQueries({ queryKey: ["projects"] });
      navigate(`/projects/${project.id}/model/trace`);
    },
  });
  const scaffoldAndImport = useMutation({
    mutationFn: async () => {
      const generated = await api.scaffoldBundleFromTask({ name, content: sourceText });
      const imported = await api.importBundle(generated.bundle.bundle_path);
      return imported;
    },
    onSuccess: ({ project }) => {
      void queryClient.invalidateQueries({ queryKey: ["bundles"] });
      void queryClient.invalidateQueries({ queryKey: ["projects"] });
      navigate(`/projects/${project.id}/model/trace`);
    },
  });
  const ready = useMemo(() => projectId !== null, [projectId]);
  const bundleItems = bundles.data?.items ?? [];
  const llmAvailable = llmUseMock || !!llmStatus.data?.available;
  const resourceItems = resources.data?.items ?? [];
  const readyResourceCount = resourceItems.filter((item) => item.extraction_status === "ready" || item.extraction_status === "needs_attention").length;
  const activeRevision = project.data?.project.current_revision ?? projectRevision;
  const manifestSummary = sourceManifest.data?.manifest.summary;
  const hasSources = sourceText.trim() !== "" || selectedFiles.length > 0 || readyResourceCount > 0;
  const canStart = name.trim() !== "" && hasSources && !start.isPending && !job;

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1 className="page-title">New Project</h1>
          <p className="page-subtitle">Create a database modeling project from task sources.</p>
        </div>
        <Button onClick={() => navigate("/projects")}>Projects</Button>
      </div>

      <div className="intake-layout">
        <Panel title="Start a modeling project">
          <div className="field" style={{ gap: 16 }}>
            <div className="intake-step">
              <span className="step-number">1</span>
              <div className="field" style={{ gap: 10 }}>
                <Field label="Project name">
                  <input className="input" value={name} placeholder="e.g. Pizzeria ordering system" onChange={(event) => setName(event.target.value)} disabled={ready} />
                </Field>
                <Field label="Description (optional)">
                  <input className="input" value={description} onChange={(event) => setDescription(event.target.value)} disabled={ready} />
                </Field>
              </div>
            </div>
            <div className="intake-step">
              <span className="step-number">2</span>
              <div className="field" style={{ gap: 10 }}>
                <Field label="Task text">
                  <textarea
                    className="textarea intake-text"
                    value={sourceText}
                    placeholder="Paste the project assignment (Serbian Cyrillic or Latin, or English)…"
                    onChange={(event) => { setSourceText(event.target.value); setTextAdded(false); }}
                  />
                </Field>
                <Field label="…or upload documents (PDF, DOCX, TXT, MD, JSON, CSV, XML)">
                  <input
                    className="input"
                    type="file"
                    multiple
                    accept=".txt,.md,.markdown,.json,.csv,.xml,.pdf,.docx,text/plain,text/markdown,application/json,text/csv,application/pdf,application/vnd.openxmlformats-officedocument.wordprocessingml.document"
                    onChange={(event) => setSelectedFiles(Array.from(event.target.files ?? []))}
                    disabled={start.isPending}
                  />
                </Field>
              </div>
            </div>
            <div className="intake-step">
              <span className="step-number">3</span>
              <div className="field" style={{ gap: 8 }}>
                <div className="toolbar">
                  <Button variant="primary" className="large" onClick={() => start.mutate()} disabled={!canStart}>
                    <Play size={18} />
                    {start.isPending ? `${phase}…` : "Start analysis"}
                  </Button>
                  {readyResourceCount > 0 && <Badge tone="good">{readyResourceCount} source{readyResourceCount === 1 ? "" : "s"} attached</Badge>}
                </div>
                <p className="muted small">The pipeline runs automatically and stops only where your review is needed.</p>
              </div>
            </div>
            {(start.isError || addText.isError || upload.isError || process.isError) && (
              <p className="error-text">{((start.error ?? addText.error ?? upload.error ?? process.error) as Error).message}</p>
            )}
            {job && projectId && (
              <JobProgress
                projectId={projectId}
                job={job}
                onDone={() => {
                  requestAutoRun(projectId);
                  navigate(`/projects/${projectId}/analysis/overview`);
                }}
              />
            )}
            <details className="dev-tools">
              <summary>Developer tools</summary>
              <div className="field" style={{ gap: 14, marginTop: 12 }}>
                <div className="llm-box">
                  <div className="bundle-main">
                    <div className="toolbar">
                      <Bot size={18} />
                      <strong>LLM settings for source segmentation</strong>
                      <Badge tone={llmAvailable ? "good" : "warn"}>{llmUseMock ? "mock" : llmStatus.data?.available ? "ready" : "no key"}</Badge>
                    </div>
                    <label className="toolbar toggle-label">
                      <input type="checkbox" checked={llmUseMock} onChange={(event) => setLlmUseMock(event.target.checked)} />
                      Mock LLM
                    </label>
                  </div>
                  <Field label="Model">
                    <input className="input" value={llmModel} onChange={(event) => setLlmModel(event.target.value)} disabled={llmUseMock || start.isPending} />
                  </Field>
                  {llmStatus.isError && <p className="error-text">{(llmStatus.error as Error).message}</p>}
                </div>
                <div className="toolbar">
                  <Button onClick={() => scaffoldAndImport.mutate()} disabled={sourceText.trim() === "" || scaffoldAndImport.isPending}>
                    <Database size={18} /> Generate Scaffold
                  </Button>
                </div>
                {scaffoldAndImport.isError && <p className="error-text">{(scaffoldAndImport.error as Error).message}</p>}
                <Field label="Import existing v0.5 bundle path">
                  <input className="input" value={bundlePath} onChange={(event) => setBundlePath(event.target.value)} />
                </Field>
                <div className="toolbar">
                  <Button onClick={() => importExisting.mutate(bundlePath)} disabled={bundlePath.trim() === "" || importExisting.isPending}>
                    <FolderOpen size={18} />
                    Import Path
                  </Button>
                </div>
                {importExisting.isError && <p className="error-text">{(importExisting.error as Error).message}</p>}
                <div className="bundle-list">
                  {bundles.isLoading && <p className="muted">Loading bundles...</p>}
                  {bundleItems.map((bundle) => (
                    <BundleCard bundle={bundle} importing={importExisting.isPending} key={bundle.id} onImport={(path) => importExisting.mutate(path)} />
                  ))}
                </div>
              </div>
            </details>
          </div>
        </Panel>

        <div className="field" style={{ gap: 16 }}>
          {ready ? (
            <Panel title="Sources">
              <div className="field" style={{ gap: 14 }}>
                <div className="toolbar">
                  <FileUp size={18} />
                  <StatusBadge value={project.data?.artifact_health.combined_document_status ?? "not_generated"} />
                  <span className="muted small">{manifestSummary?.total ?? resourceItems.length} resources · {project.data?.project.counts.combined_sentences ?? 0} sentences · rev {activeRevision}</span>
                </div>
                {resources.isLoading ? (
                  <LoadingState />
                ) : (
                  <ResourceTable
                    items={resourceItems}
                    removing={removeResource.isPending}
                    onPreview={setSelectedResource}
                    onDelete={(resourceId) => removeResource.mutate(resourceId)}
                  />
                )}
              </div>
            </Panel>
          ) : null}
          <Panel title="How the analysis works">
            <ol className="how-it-works">
              <li><Sparkles size={16} /><div><strong>Sources</strong><span>The LLM splits the text into traceable segments; each becomes a source unit with a stable ID.</span></div></li>
              <li className="gate"><UserCheck size={16} /><div><strong>Your review</strong><span>Only segments the segmentation flagged as unclear wait for your decision.</span></div></li>
              <li><Sparkles size={16} /><div><strong>Conceptual model</strong><span>The LLM describes what the system must remember; entities, attributes and relationships are derived from it.</span></div></li>
              <li className="gate"><UserCheck size={16} /><div><strong>Your acceptance</strong><span>You review the ER diagram before it becomes a database model.</span></div></li>
              <li><CheckCircle2 size={16} /><div><strong>Database model</strong><span>Deterministic rules produce DB-DSL, validated and traced to the source; after your final acceptance DBML is generated.</span></div></li>
            </ol>
          </Panel>
        </div>
      </div>
      {selectedResource && (
        <Drawer title={selectedResource.title} onClose={() => setSelectedResource(null)}>
          <div className="toolbar">
            <StatusBadge value={selectedResource.extraction_status} />
            <Badge>{selectedResource.file_type}</Badge>
            <Badge>{selectedResource.line_count ?? 0} lines</Badge>
          </div>
          <p className="muted">{selectedResource.content_hash ? shortHash(selectedResource.content_hash) : "No content hash"}</p>
          {resourcePreview.isLoading ? (
            <LoadingState label="Loading text" />
          ) : resourcePreview.isError ? (
            <p className="error-text">{(resourcePreview.error as Error).message}</p>
          ) : (
            <pre className="code-view">{resourcePreview.data?.text ?? ""}</pre>
          )}
        </Drawer>
      )}
    </div>
  );
}

function ResourceTable({
  items,
  removing,
  onPreview,
  onDelete,
}: {
  items: InputResource[];
  removing: boolean;
  onPreview: (resource: InputResource) => void;
  onDelete: (resourceId: string) => void;
}) {
  if (items.length === 0) {
    return <p className="muted">No resources added.</p>;
  }
  return (
    <table className="data-table">
      <thead>
        <tr>
          <th>Resource</th>
          <th>Status</th>
          <th>Hash</th>
          <th style={{ width: 145 }}>Actions</th>
        </tr>
      </thead>
      <tbody>
        {items.map((item) => (
          <tr key={item.id}>
            <td>
              <strong>{item.title}</strong>
              <div className="muted truncate">
                {item.id} · {item.file_name || item.kind} · {formatBytes(item.size_bytes ?? 0)}
              </div>
            </td>
            <td>
              <StatusBadge value={item.extraction_status} />
              {item.warnings && item.warnings.length > 0 && <div className="muted">{item.warnings.length} warnings</div>}
            </td>
            <td className="truncate">{item.content_hash ? shortHash(item.content_hash) : "-"}</td>
            <td>
              <div className="toolbar">
                <Button variant="ghost" onClick={() => onPreview(item)} aria-label={`Preview ${item.title}`}>
                  <Eye size={16} />
                </Button>
                <Button variant="ghost" disabled={removing} onClick={() => onDelete(item.id)} aria-label={`Delete ${item.title}`}>
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

function shortHash(value: string) {
  return value.replace(/^sha256:/, "").slice(0, 12);
}

function formatBytes(value: number) {
  if (!value) return "0 B";
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / (1024 * 1024)).toFixed(1)} MB`;
}

function BundleCard({
  bundle,
  importing,
  onImport,
}: {
  bundle: BundleCandidate;
  importing: boolean;
  onImport: (path: string) => void;
}) {
  return (
    <div className="bundle-item">
      <div className="bundle-main">
        <div>
          <strong>{bundle.name}</strong>
          <div className="muted truncate">{bundle.bundle_path}</div>
        </div>
        <Button onClick={() => onImport(bundle.model_path)} disabled={importing}>
          <Database size={16} />
          Import
        </Button>
      </div>
      <div className="toolbar">
        <Badge>{bundle.source_units} sources</Badge>
        <Badge>{bundle.entities} tables</Badge>
        <Badge>{bundle.relationships} rels</Badge>
        <Badge tone={bundle.validation_errors ? "bad" : "good"}>{bundle.validation_errors} errors</Badge>
        <Badge tone={bundle.lint_warnings ? "warn" : "good"}>{bundle.lint_warnings} warnings</Badge>
      </div>
    </div>
  );
}
