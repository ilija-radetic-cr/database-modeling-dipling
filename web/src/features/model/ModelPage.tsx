import { memo, useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import ReactFlow, {
  Background,
  Controls,
  Handle,
  MarkerType,
  Position,
  type Edge,
  type Node,
  type NodeProps,
} from "reactflow";
import { AlertTriangle, ArrowRight, CheckCircle2, Play, Search } from "lucide-react";
import { api } from "@/shared/api/client";
import type { Job, ModelNode, QualityIssue, SourceUnit } from "@/shared/api/types";
import { Badge, Button, Drawer, LoadingState, Panel, StatusBadge } from "@/shared/components/ui";
import { useRouter } from "@/shared/lib/router";
	import { JobProgress } from "@/features/jobs/JobProgress";
	import { PipelineStepper } from "@/features/analysis/PipelineStepper";
	import { finalModelAction } from "@/shared/lib/pipeline";

const nodeTypes = { tableNode: memo(TableNode) };

export function ModelPage({ projectId, mode }: { projectId: string; mode: string }) {
  const { navigate } = useRouter();
	const queryClient = useQueryClient();
  const project = useQuery({ queryKey: ["project", projectId], queryFn: () => api.getProject(projectId) });
	const [job, setJob] = useState<Job | null>(null);
	const accept = useMutation({
		mutationFn: () => api.acceptModel(projectId, project.data?.project.current_revision ?? 0),
		onSuccess: () => void queryClient.invalidateQueries(),
	});
	const generateOutputs = useMutation({
		mutationFn: () => api.runStage(projectId, "generate_outputs", project.data?.project.current_revision ?? 0),
		onSuccess: ({ job: started }) => setJob(started),
	});
	const health = project.data?.artifact_health;
	const modelReady = health?.model_status === "ready";
	const nextAction = finalModelAction({ modelReady, accepted: !!health?.final_model_accepted, dbmlReady: health?.dbml_status === "ready" });
	const action = nextAction === "return_to_pipeline" ? (
		<Button onClick={() => navigate(`/projects/${projectId}/analysis/overview`)}>Return to pipeline</Button>
	) : nextAction === "accept_model" ? (
		<Button variant="primary" disabled={accept.isPending} onClick={() => accept.mutate()}><CheckCircle2 size={18} />Accept Final Model</Button>
	) : nextAction === "generate_outputs" ? (
		<Button variant="primary" disabled={generateOutputs.isPending || !!job} onClick={() => generateOutputs.mutate()}><Play size={18} />Generate DBML & Trace</Button>
	) : (
		<Button variant="primary" onClick={() => navigate(`/projects/${projectId}/dbml`)}>Open Finalize<ArrowRight size={18} /></Button>
	);

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1 className="page-title">{project.data?.project.name ?? "Project"} | Model Workspace</h1>
          <p className="page-subtitle">Read-only model inspection, bidirectional traceability and quality issues.</p>
        </div>
		<div className="toolbar">{action}</div>
      </div>
		{health && <PipelineStepper health={health} />}
		{job && <Panel title="Generating final outputs"><JobProgress projectId={projectId} job={job} onDone={() => { setJob(null); void queryClient.invalidateQueries(); }} /></Panel>}
		{(accept.isError || generateOutputs.isError) && <p className="error-text">{(accept.error ?? generateOutputs.error) instanceof Error ? (accept.error ?? generateOutputs.error)?.message : "The action failed."}</p>}

      <div className="tabs">
		<button className={`tab ${mode === "conceptual" ? "active" : ""}`} onClick={() => navigate(`/projects/${projectId}/model/conceptual`)}>
			Conceptual
		</button>
        <button className={`tab ${mode === "trace" ? "active" : ""}`} onClick={() => navigate(`/projects/${projectId}/model/trace`)}>
          Trace View
        </button>
        <button className={`tab ${mode === "quality" ? "active" : ""}`} onClick={() => navigate(`/projects/${projectId}/model/quality`)}>
          Quality Issues
        </button>
      </div>

		{mode === "conceptual" ? <ConceptualView projectId={projectId} /> : mode === "quality" ? <QualityView projectId={projectId} /> : <TraceView projectId={projectId} />}
    </div>
  );
}

function ConceptualView({ projectId }: { projectId: string }) {
	const queryClient = useQueryClient();
	const project = useQuery({ queryKey: ["project", projectId], queryFn: () => api.getProject(projectId) });
	const conceptual = useQuery({ queryKey: ["conceptual-model", projectId], queryFn: () => api.conceptualModel(projectId), retry: false });
	const accept = useMutation({
		mutationFn: () => api.acceptConceptualModel(projectId, project.data?.project.current_revision ?? 0),
		onSuccess: () => void queryClient.invalidateQueries(),
	});
	if (conceptual.isLoading) return <LoadingState />;
	if (conceptual.isError) return <Panel title="Conceptual model is not ready"><p className="muted">Complete analysis and resolve blocking decisions first.</p></Panel>;
	const model = conceptual.data!.conceptual_model;
	return (
		<div>
			<Panel title="Conceptual decision gate" action={!conceptual.data?.accepted ? <Button variant="primary" disabled={accept.isPending || !conceptual.data?.qa.ok} onClick={() => accept.mutate()}><CheckCircle2 size={18} />Accept Conceptual Model</Button> : <StatusBadge value="accepted" />}>
				<p>Inspect identities, attributes, cardinalities and obligation coverage before logical projection.</p>
				{accept.isError && <p className="error-text">{accept.error instanceof Error ? accept.error.message : "Acceptance failed."}</p>}
			</Panel>
		<div className="grid-2">
			<Panel title={`Entity concepts · ${model.entity_concepts.length}`}>
				<div className="field" style={{ gap: 10 }}>
					{model.entity_concepts.map((entity) => (
						<div className="review-option-card" key={entity.id}>
							<div className="toolbar"><strong>{entity.label}</strong><Badge>{entity.kind}</Badge><Badge>{entity.id}</Badge></div>
							<p>{entity.description}</p>
							<p className="muted">Attributes: {entity.attributes.map((attribute) => attribute.label).join(", ") || "none"}</p>
							<div className="toolbar">{entity.evidence.source_units.map((id) => <Badge key={id}>{id}</Badge>)}</div>
						</div>
					))}
				</div>
			</Panel>
			<Panel title={`Relationships · ${model.relationships.length}`}>
				<div className="field" style={{ gap: 10 }}>
					{model.relationships.map((relationship) => (
						<div className="panel" key={relationship.id}><div className="panel-body">
							<strong>{relationship.label}</strong>
							<p>{relationship.from} → {relationship.to} · {relationship.cardinality}</p>
							<p className="muted">{relationship.description}</p>
						</div></div>
					))}
					{model.relationships.length === 0 && <p className="muted">No conceptual relationships proposed.</p>}
				</div>
				{model.unresolved_review_ids.length > 0 && <p className="error-text">Unresolved: {model.unresolved_review_ids.join(", ")}</p>}
			</Panel>
		</div></div>
	);
}

function TraceView({ projectId }: { projectId: string }) {
	const { navigate } = useRouter();
  const [hoveredSource, setHoveredSource] = useState<string | null>(null);
  const [selectedSource, setSelectedSource] = useState<string | null>(null);
  const [selectedElement, setSelectedElement] = useState<string | null>(null);
  const [drawerElement, setDrawerElement] = useState<string | null>(null);
	const [correctionType, setCorrectionType] = useState("wrong_relationship_cardinality");
	const [correctionNote, setCorrectionNote] = useState("");
  const sourceUnitRefs = useRef<Record<string, HTMLDivElement | null>>({});
  const lastScrolledSource = useRef<string | null>(null);
  const graph = useQuery({ queryKey: ["model-graph", projectId], queryFn: () => api.modelGraph(projectId), retry: false });
  const trace = useQuery({ queryKey: ["trace-index", projectId], queryFn: () => api.traceIndex(projectId), retry: false });
  const sources = useQuery({ queryKey: ["source-units", projectId, "all", ""], queryFn: () => api.sourceUnits(projectId) });
  const details = useQuery({
    queryKey: ["model-element", projectId, drawerElement],
    queryFn: () => api.modelElement(projectId, drawerElement!),
    enabled: !!drawerElement,
  });
	const project = useQuery({ queryKey: ["project", projectId], queryFn: () => api.getProject(projectId) });
	const correction = useMutation({
		mutationFn: () => api.requestModelCorrection(projectId, {
			base_revision: project.data?.project.current_revision ?? 0,
			element_id: drawerElement ?? "",
			correction_type: correctionType,
			note: correctionNote,
		}),
		onSuccess: () => navigate(`/projects/${projectId}/analysis/review`),
	});

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        setSelectedSource(null);
        setSelectedElement(null);
        setDrawerElement(null);
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  const selectedSources = selectedElement
    ? trace.data?.trace_index.element_to_sources[selectedElement] ?? []
    : selectedSource
      ? [selectedSource]
      : [];

  useEffect(() => {
    if (!selectedElement) return;
    const firstSourceID = selectedSources[0];
    if (!firstSourceID || lastScrolledSource.current === firstSourceID) return;
    const target = sourceUnitRefs.current[firstSourceID];
    if (!target) return;
    lastScrolledSource.current = firstSourceID;
    target.scrollIntoView({ behavior: "smooth", block: "center" });
  }, [selectedElement, selectedSources]);

  if (graph.isError || trace.isError) {
    return (
      <Panel title="Model is not generated">
        <p className="muted">Resolve review questions and generate the database model from Analysis Workspace.</p>
      </Panel>
    );
  }
  if (graph.isLoading || trace.isLoading || sources.isLoading) {
    return <LoadingState />;
  }

  const highlightedElements = hoveredSource
    ? trace.data?.trace_index.source_to_elements[hoveredSource] ?? []
    : selectedSource
      ? trace.data?.trace_index.source_to_elements[selectedSource] ?? []
      : selectedElement
        ? [selectedElement]
        : [];

  if ((graph.data?.model_graph.nodes.length ?? 0) === 0) {
    return (
      <Panel title="Diagram data is empty">
        <p className="muted">The model graph endpoint returned no table nodes.</p>
      </Panel>
    );
  }

  const highlightedCount = highlightedElements.length;

  const highlightedInfo =
    highlightedCount > 0
      ? `${highlightedCount} model element${highlightedCount === 1 ? "" : "s"} highlighted`
      : "Hover or click a source unit or model element";

  const selectedInfo = selectedElement ?? selectedSource ?? "Nothing pinned";

  const selectedSourcesSet = new Set(selectedSources);

  const highlightedElementsSet = new Set(highlightedElements);

  const graphNodes = graph.data?.model_graph.nodes ?? [];
  const graphEdges = graph.data?.model_graph.edges ?? [];

  const selectedElementID = selectedElement;

  const handleSourceSelect = (sourceID: string) => {
    lastScrolledSource.current = null;
    setSelectedSource(sourceID);
    setSelectedElement(null);
    setDrawerElement(null);
  };

  const handleElementSelect = (elementID: string) => {
    lastScrolledSource.current = null;
    setSelectedElement(elementID);
    setSelectedSource(null);
    setDrawerElement(elementID);
  };

  return (
    <div className="split-workspace">
      <Panel title="Source Units" action={<Badge>{selectedInfo}</Badge>}>
        <div className="source-pane">
          {(sources.data?.items ?? []).map((unit) => (
            <SourceUnitRow
              key={unit.id}
              unit={unit}
              highlighted={selectedSourcesSet.has(unit.id) || hoveredSource === unit.id}
              onHover={setHoveredSource}
              onSelect={() => handleSourceSelect(unit.id)}
              sourceUnitRef={(node) => {
                sourceUnitRefs.current[unit.id] = node;
              }}
            />
          ))}
        </div>
      </Panel>

      <Panel title="Database Diagram" action={<Badge tone={highlightedCount > 0 ? "good" : "default"}>{highlightedInfo}</Badge>}>
        <div className="diagram-pane">
          <ReactFlow
            nodes={toFlowNodes(graphNodes, [...highlightedElementsSet], selectedElementID)}
            edges={toFlowEdges(graphEdges, [...highlightedElementsSet])}
            nodeTypes={nodeTypes}
            onNodeClick={(_, node) => handleElementSelect(node.id)}
            onEdgeClick={(_, edge) => handleElementSelect(edge.id)}
            fitView
            minZoom={0.2}
          >
            <Background />
            <Controls />
          </ReactFlow>
        </div>
      </Panel>

      {drawerElement && (
        <Drawer title={drawerElement} onClose={() => setDrawerElement(null)}>
          {details.isLoading ? (
            <LoadingState />
          ) : (
            <>
              <StatusBadge value={details.data?.model_element.kind ?? "element"} />
              <p>{details.data?.model_element.description || details.data?.model_element.expression}</p>
              <Panel title="Evidence">
                <div className="toolbar">
                  {(details.data?.model_element.evidence.source_units ?? []).map((id) => (
                    <Badge key={id}>{id}</Badge>
                  ))}
                </div>
              </Panel>
              <Panel title="Related">
                <div className="toolbar">
                  {(details.data?.model_element.related_elements ?? []).map((id) => (
                    <Badge key={id}>{id}</Badge>
                  ))}
                </div>
              </Panel>
			<Panel title="Controlled correction">
				<div className="field" style={{ gap: 10 }}>
					<select className="select" value={correctionType} onChange={(event) => setCorrectionType(event.target.value)}>
						<option value="wrong_entity">Wrong entity</option>
						<option value="missing_entity">Missing entity</option>
						<option value="wrong_attribute">Wrong attribute</option>
						<option value="wrong_relationship_cardinality">Wrong relationship / cardinality</option>
						<option value="wrong_constraint">Wrong constraint</option>
						<option value="wrong_persistence">Wrong persistence</option>
						<option value="missing_evidence">Missing evidence</option>
						<option value="other">Other</option>
					</select>
					<textarea className="textarea" placeholder="Explain the correction and expected result" value={correctionNote} onChange={(event) => setCorrectionNote(event.target.value)} />
					<Button onClick={() => correction.mutate()} disabled={correction.isPending || correctionNote.trim() === ""}>Request model correction</Button>
					{correction.isError && <p className="error-text">{correction.error instanceof Error ? correction.error.message : "Correction request failed."}</p>}
				</div>
			</Panel>
            </>
          )}
        </Drawer>
      )}
    </div>
  );
}

function SourceUnitRow({
  unit,
  highlighted,
  onHover,
  onSelect,
  sourceUnitRef,
}: {
  unit: SourceUnit;
  highlighted: boolean;
  onHover: (id: string | null) => void;
  onSelect: () => void;
  sourceUnitRef: (node: HTMLDivElement | null) => void;
}) {
  return (
    <div
      ref={sourceUnitRef}
      className={`source-unit ${highlighted ? "highlighted" : ""}`}
      onMouseEnter={() => onHover(unit.id)}
      onMouseLeave={() => onHover(null)}
      onClick={onSelect}
    >
      <div className="toolbar">
        <strong>{unit.id}</strong>
        <Badge>{unit.kind}</Badge>
      </div>
      <p>{unit.normalized_text}</p>
    </div>
  );
}

function TableNode({ data, selected }: NodeProps<{ model: ModelNode; highlighted: string[] }>) {
  const model = data.model;
  const highlighted = data.highlighted;
  return (
    <div className={`table-node ${selected || highlighted.includes(model.id) ? "selected" : ""}`}>
      <Handle type="target" position={Position.Left} />
      <div className="table-node-title">{model.label}</div>
      {(model.fields ?? []).slice(0, 9).map((field) => (
        <div className={`field-row ${highlighted.includes(field.element_id) ? "highlighted" : ""}`} key={field.element_id}>
          <span className="truncate">{field.id}</span>
          <span className="muted">{field.type}</span>
        </div>
      ))}
      {(model.fields?.length ?? 0) > 9 && <div className="field-row muted">+ {(model.fields?.length ?? 0) - 9} more</div>}
      <Handle type="source" position={Position.Right} />
    </div>
  );
}

function toFlowNodes(models: ModelNode[], highlighted: string[], selected: string | null): Node[] {
  const columns = 4;
  return models.map((model, index) => ({
    id: model.id,
    type: "tableNode",
    position: { x: (index % columns) * 300, y: Math.floor(index / columns) * 280 },
    data: { model, highlighted },
    selected: selected === model.id,
    draggable: true,
    selectable: true,
    style: { width: 230 },
  })) as Node[];
}

function toFlowEdges(models: { id: string; from: string; to: string; cardinality: string }[], highlighted: string[]): Edge[] {
  return models.map((edge) => ({
    id: edge.id,
    source: edge.from,
    target: edge.to,
    label: edge.cardinality,
    markerEnd: { type: MarkerType.ArrowClosed },
    animated: highlighted.includes(edge.id),
    style: { stroke: highlighted.includes(edge.id) ? "#0f766e" : "#94a3b8", strokeWidth: highlighted.includes(edge.id) ? 2.5 : 1.5 },
  }));
}

function QualityView({ projectId }: { projectId: string }) {
  const { navigate } = useRouter();
  const queryClient = useQueryClient();
  const quality = useQuery({ queryKey: ["quality", projectId], queryFn: () => api.quality(projectId) });
	const semantic = useQuery({ queryKey: ["semantic-verification", projectId], queryFn: () => api.semanticVerification(projectId), retry: false });
  const accept = useMutation({
    mutationFn: (id: string) => api.acceptQuality(projectId, id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["quality", projectId] }),
  });

  if (quality.isLoading) return <LoadingState />;
  const report = quality.data?.quality;
  return (
    <div className="page">
      <div className="grid-3">
        <MetricWithIcon label="Validation errors" value={report?.summary.validation_errors ?? 0} bad={(report?.summary.validation_errors ?? 0) > 0} />
        <MetricWithIcon label="Lint warnings" value={report?.summary.lint_warnings ?? 0} bad={false} />
        <MetricWithIcon label="Blocking issues" value={report?.summary.blocking_issues ?? 0} bad={(report?.summary.blocking_issues ?? 0) > 0} />
      </div>
		{semantic.data?.semantic_verification && (
			<Panel title="Semantic obligation gate">
				<div className="toolbar">
					<Badge tone={semantic.data.semantic_verification.ok ? "good" : "bad"}>{semantic.data.semantic_verification.ok ? "Passed" : "Blocked"}</Badge>
					<Badge>{semantic.data.semantic_verification.obligations_realized}/{semantic.data.semantic_verification.obligations_required} required obligations realized</Badge>
					<Badge tone={semantic.data.semantic_verification.blocking_issues ? "bad" : "good"}>{semantic.data.semantic_verification.blocking_issues} blocking issues</Badge>
				</div>
				{semantic.data.semantic_verification.issues.map((issue) => <p key={issue.id} className={issue.severity === "high" || issue.severity === "critical" ? "error-text" : "muted"}>{issue.id} · {issue.code}: {issue.message}</p>)}
			</Panel>
		)}
      <Panel title="Quality Issues">
        <table className="data-table">
          <thead>
            <tr>
              <th>Issue</th>
              <th>Severity</th>
              <th>Element</th>
              <th>Message</th>
              <th style={{ width: 210 }}>Actions</th>
            </tr>
          </thead>
          <tbody>
            {(report?.issues ?? []).map((issue: QualityIssue) => (
              <tr key={issue.id}>
                <td>
                  <strong>{issue.id}</strong>
                  <div className="muted">{issue.code}</div>
                </td>
                <td>
                  <StatusBadge value={issue.severity} />
                </td>
                <td>{issue.element_id}</td>
                <td>{issue.message}</td>
                <td>
                  <div className="toolbar">
                    {issue.element_id && (
                      <Button onClick={() => navigate(`/projects/${projectId}/model/trace`)}>
                        <Search size={16} />
                        Inspect
                      </Button>
                    )}
                    {issue.severity !== "error" && !issue.accepted && (
                      <Button onClick={() => accept.mutate(issue.id)}>Accept</Button>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </Panel>
    </div>
  );
}

function MetricWithIcon({ label, value, bad }: { label: string; value: number; bad: boolean }) {
  return (
    <div className="metric">
      <span className="metric-value">
        {bad ? <AlertTriangle size={20} color="#b42318" /> : <CheckCircle2 size={20} color="#0f766e" />} {value}
      </span>
      <span className="metric-label">{label}</span>
    </div>
  );
}
