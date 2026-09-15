import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, Database, Eye, FileText, FileUp, FolderOpen, Play, Plus, Sparkles, Trash2, UploadCloud } from "lucide-react";
import { api } from "@/shared/api/client";
import type { BundleCandidate, InputResource, Job } from "@/shared/api/types";
import { Badge, Button, Drawer, Field, LoadingState, Metric, Panel, StatusBadge } from "@/shared/components/ui";
import { useRouter } from "@/shared/lib/router";
import { JobProgress } from "@/features/jobs/JobProgress";
	import { newProjectActionState } from "@/shared/lib/pipeline";

export function NewProjectPage() {
  const { navigate } = useRouter();
  const queryClient = useQueryClient();
  const [projectId, setProjectId] = useState<string | null>(null);
  const [projectRevision, setProjectRevision] = useState(1);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [sourceTitle, setSourceTitle] = useState("Main task text");
	const [sourceText, setSourceText] = useState("");
	const [bundlePath, setBundlePath] = useState("");
  const [llmModel, setLlmModel] = useState("gpt-5.6-sol");
  const [llmUseMock, setLlmUseMock] = useState(false);
  const [job, setJob] = useState<Job | null>(null);
  const [selectedFiles, setSelectedFiles] = useState<File[]>([]);
  const [selectedResource, setSelectedResource] = useState<InputResource | null>(null);

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
  const combinedDocument = useQuery({
    queryKey: ["combined-document", projectId],
    queryFn: () => api.combinedDocument(projectId ?? ""),
    enabled: projectId !== null && project.data?.artifact_health.combined_document_status === "ready",
  });
  const resourcePreview = useQuery({
    queryKey: ["resource-text", projectId, selectedResource?.id],
    queryFn: () => api.resourceText(projectId ?? "", selectedResource?.id ?? ""),
    enabled: projectId !== null && selectedResource !== null,
  });
  const create = useMutation({
    mutationFn: () => api.createProject({ name, description, language: "sr-Cyrl", domain: "information_system" }),
    onSuccess: ({ project }) => {
      setProjectId(project.id);
      setProjectRevision(project.current_revision);
      void queryClient.invalidateQueries({ queryKey: ["projects"] });
    },
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
        model: llmUseMock ? "mock-model" : llmModel.trim() || llmStatus.data?.default_model || "gpt-5.6-sol",
        reasoning_effort: "low",
        max_output_tokens: 12000,
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
  const llmDraftAndImport = useMutation({
    mutationFn: async () => {
      const model = llmUseMock ? "mock-model" : llmModel.trim() || llmStatus.data?.default_model || "gpt-5.6-sol";
      const generated = await api.llmPlanBundleFromTask({
        name,
        content: sourceText,
        model,
        reasoning_effort: "medium",
        max_output_tokens: 12000,
        max_repair_attempts: 1,
        mock: llmUseMock,
      });
      return api.importBundle(generated.bundle.bundle_path);
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
	const actionState = newProjectActionState({ created: ready, name, sourceText, readyResources: readyResourceCount, llmAvailable });

  async function addTextToCurrentProject() {
    if (!projectId) return;
    await addText.mutateAsync(projectId);
  }

  async function uploadSelectedFiles() {
    if (!projectId) return;
    await upload.mutateAsync(projectId);
  }

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1 className="page-title">New Project</h1>
          <p className="page-subtitle">Create a database modeling project from task sources and structured examples.</p>
        </div>
        <Button onClick={() => navigate("/projects")}>Projects</Button>
      </div>

      <div className="grid-2">
        <Panel title="Project setup">
          <div className="field" style={{ gap: 14 }}>
            <Field label="Project name">
              <input className="input" value={name} onChange={(event) => setName(event.target.value)} />
            </Field>
            <Field label="Description">
              <textarea className="textarea" value={description} onChange={(event) => setDescription(event.target.value)} />
            </Field>
            <Field label="Source title">
              <input className="input" value={sourceTitle} onChange={(event) => setSourceTitle(event.target.value)} />
            </Field>
            <Field label="Paste text">
              <textarea className="textarea" value={sourceText} onChange={(event) => setSourceText(event.target.value)} />
            </Field>
            <div className="toolbar">
			<Button variant="primary" onClick={() => create.mutate()} disabled={create.isPending || !actionState.canCreate}>
                <Plus size={18} />
				Create Project
              </Button>
			<Button onClick={addTextToCurrentProject} disabled={addText.isPending || !actionState.canAddText}>
                <FileText size={18} />
                Add Text
              </Button>
			<Button
				variant="primary"
                onClick={() => {
                  if (projectId) void process.mutateAsync(projectId);
                }}
				disabled={!actionState.canBuildCombinedDocument || process.isPending || !!job}
              >
                <Play size={18} />
				Build Combined Document
              </Button>
            </div>
            <Field label="Upload documents">
              <input
                className="input"
                type="file"
                multiple
                accept=".txt,.md,.markdown,.json,.csv,.xml,.pdf,.docx,text/plain,text/markdown,application/json,text/csv,application/pdf,application/vnd.openxmlformats-officedocument.wordprocessingml.document"
                onChange={(event) => setSelectedFiles(Array.from(event.target.files ?? []))}
                disabled={!projectId || upload.isPending}
              />
            </Field>
            <div className="toolbar">
              <Button onClick={uploadSelectedFiles} disabled={!projectId || selectedFiles.length === 0 || upload.isPending}>
                <UploadCloud size={18} />
                Upload {selectedFiles.length > 0 ? selectedFiles.length : ""}
              </Button>
              <Badge tone={readyResourceCount ? "good" : "default"}>{readyResourceCount} ready resources</Badge>
              <Badge>rev {activeRevision}</Badge>
            </div>
            {scaffoldAndImport.isError && <p className="error-text">{(scaffoldAndImport.error as Error).message}</p>}
            {addText.isError && <p className="error-text">{(addText.error as Error).message}</p>}
            {upload.isError && <p className="error-text">{(upload.error as Error).message}</p>}
            {process.isError && <p className="error-text">{(process.error as Error).message}</p>}
			<details>
				<summary><strong>Developer / Experiments</strong></summary>
			<div className="llm-box" style={{ marginTop: 12 }}>
              <div className="bundle-main">
				<div className="toolbar">
					<Button onClick={() => scaffoldAndImport.mutate()} disabled={sourceText.trim() === "" || scaffoldAndImport.isPending}>
						<Database size={18} /> Generate Scaffold
					</Button>
                  <Bot size={18} />
                  <strong>LLM Draft</strong>
                  <Badge tone={llmAvailable ? "good" : "warn"}>{llmUseMock ? "mock" : llmStatus.data?.available ? "ready" : "no key"}</Badge>
                </div>
                <label className="toolbar toggle-label">
                  <input type="checkbox" checked={llmUseMock} onChange={(event) => setLlmUseMock(event.target.checked)} />
                  Mock
                </label>
              </div>
              <Field label="Model">
                <input
                  className="input"
                  value={llmModel}
                  onChange={(event) => setLlmModel(event.target.value)}
                  disabled={llmUseMock || llmDraftAndImport.isPending}
                />
              </Field>
              <div className="toolbar">
                <Button
                  variant="primary"
                  onClick={() => llmDraftAndImport.mutate()}
                  disabled={sourceText.trim() === "" || !llmAvailable || llmDraftAndImport.isPending}
                >
                  <Sparkles size={18} />
                  {llmDraftAndImport.isPending ? "Generating LLM Draft..." : "Generate LLM Draft"}
                </Button>
              </div>
              {llmStatus.isError && <p className="error-text">{(llmStatus.error as Error).message}</p>}
              {llmDraftAndImport.isError && <p className="error-text">{(llmDraftAndImport.error as Error).message}</p>}
			</div>
			</details>
            {job && projectId && (
			<JobProgress projectId={projectId} job={job} onDone={() => navigate(`/projects/${projectId}/analysis/overview`)} />
            )}
          </div>
        </Panel>

        <div className="field" style={{ gap: 16 }}>
		<Panel title="Developer / Experiments">
			<details>
				<summary><strong>Import Existing v0.5 Bundle</strong></summary>
            <div className="field" style={{ gap: 14 }}>
              <Field label="Bundle path">
                <input className="input" value={bundlePath} onChange={(event) => setBundlePath(event.target.value)} />
              </Field>
              <div className="toolbar">
                <Button
                  variant="primary"
                  onClick={() => importExisting.mutate(bundlePath)}
                  disabled={bundlePath.trim() === "" || importExisting.isPending}
                >
                  <FolderOpen size={18} />
                  Import Path
                </Button>
              </div>
              {importExisting.isError && <p className="error-text">{(importExisting.error as Error).message}</p>}
              <div className="bundle-list">
                {bundles.isLoading && <p className="muted">Loading bundles...</p>}
                {!bundles.isLoading && bundleItems.length === 0 && <p className="muted">No v0.5 bundles found in poc.</p>}
                {bundleItems.map((bundle) => (
                  <BundleCard
                    bundle={bundle}
                    importing={importExisting.isPending}
                    key={bundle.id}
                    onImport={(path) => importExisting.mutate(path)}
                  />
                ))}
              </div>
			</div>
			</details>
          </Panel>

          <Panel title="Resource Intake">
            <div className="field" style={{ gap: 14 }}>
              <div className="grid-3 compact-metrics">
                <Metric label="Resources" value={manifestSummary?.total ?? resourceItems.length} />
                <Metric label="Ready" value={manifestSummary?.ready ?? readyResourceCount} />
                <Metric label="OD Sentences" value={project.data?.project.counts.combined_sentences ?? 0} />
              </div>
              <div className="toolbar">
                <FileUp size={18} />
                <span>Manifest</span>
                <StatusBadge value={project.data?.artifact_health.source_manifest_status ?? "not_generated"} />
                <span>Combined document</span>
                <StatusBadge value={project.data?.artifact_health.combined_document_status ?? "not_generated"} />
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
              {combinedDocument.data?.combined_document && (
                <div className="artifact-preview">
                  <div className="toolbar">
                    <Badge tone="good">{combinedDocument.data.combined_document.summary.sentence_count} OD sentences</Badge>
                    <Badge tone={combinedDocument.data.combined_document.summary.warning_count ? "warn" : "good"}>
                      {combinedDocument.data.combined_document.summary.warning_count} warnings
                    </Badge>
                  </div>
                  <pre>{combinedDocument.data.combined_document.markdown}</pre>
                </div>
              )}
            </div>
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
        <Badge>{bundle.requirements} reqs</Badge>
        <Badge>{bundle.entities} tables</Badge>
        <Badge>{bundle.relationships} rels</Badge>
        <Badge tone={bundle.validation_errors ? "bad" : "good"}>{bundle.validation_errors} errors</Badge>
        <Badge tone={bundle.lint_warnings ? "warn" : "good"}>{bundle.lint_warnings} warnings</Badge>
      </div>
    </div>
  );
}
