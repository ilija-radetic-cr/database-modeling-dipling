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
import { AlertTriangle, ArrowRight, CheckCircle2, GitBranch, Play, RotateCcw, Search } from "lucide-react";
import { api } from "@/shared/api/client";
import type { ConceptualDescription, Job, ModelNode, QualityIssue, SourceUnit } from "@/shared/api/types";
import { Badge, Button, Drawer, LoadingState, Panel, StatusBadge } from "@/shared/components/ui";
import { useRouter } from "@/shared/lib/router";
	import { JobProgress } from "@/features/jobs/JobProgress";
	import { PipelineStepper } from "@/features/analysis/PipelineStepper";
	import { finalModelAction } from "@/shared/lib/pipeline";
import { finalizePath, requestAutoRun } from "@/shared/lib/autopilot";
import { normalizeConceptualModelForView } from "./conceptualModel";
import { layoutGraph } from "@/shared/lib/graphLayout";

const nodeTypes = { tableNode: memo(TableNode) };

export function ModelPage({ projectId, mode }: { projectId: string; mode: string }) {
  const { navigate } = useRouter();
	const queryClient = useQueryClient();
  const project = useQuery({ queryKey: ["project", projectId], queryFn: () => api.getProject(projectId) });
	const [job, setJob] = useState<Job | null>(null);
	const generateOutputs = useMutation({
		mutationFn: async () => {
			const fresh = await queryClient.fetchQuery({ queryKey: ["project", projectId], queryFn: () => api.getProject(projectId), staleTime: 0 });
			return api.runStage(projectId, "generate_outputs", fresh.project.current_revision);
		},
		onSuccess: ({ job: started }) => setJob(started),
	});
	// Accepting the final model is the last human gate; outputs are deterministic, so generate them right away.
	const accept = useMutation({
		mutationFn: () => api.acceptModel(projectId, project.data?.project.current_revision ?? 0),
		onSuccess: async () => {
			await queryClient.invalidateQueries();
			generateOutputs.mutate();
		},
	});
	const continuePipeline = () => {
		requestAutoRun(projectId);
		navigate(`/projects/${projectId}/analysis/overview`);
	};
	const health = project.data?.artifact_health;
	const modelReady = health?.model_status === "ready";
	const nextAction = finalModelAction({
		modelReady,
		accepted: !!health?.final_model_accepted,
		dbmlReady: health?.dbml_status === "ready",
		semanticStatus: health?.semantic_verification_status,
	});
	const action = nextAction === "return_to_pipeline" ? (
		<Button onClick={() => navigate(`/projects/${projectId}/analysis/overview`)}>Return to pipeline</Button>
	) : nextAction === "verify_semantic" ? (
		<Button variant="primary" onClick={continuePipeline}><Play size={18} />Verify Semantic Obligations</Button>
	) : nextAction === "resolve_semantic" ? (
		<Button variant="primary" onClick={() => navigate(`/projects/${projectId}/model/quality`)}><AlertTriangle size={18} />Resolve Semantic Issues</Button>
	) : nextAction === "accept_model" ? (
		<Button variant="primary" disabled={accept.isPending} onClick={() => accept.mutate()}><CheckCircle2 size={18} />Accept Final Model</Button>
	) : nextAction === "generate_outputs" ? (
		<Button variant="primary" disabled={generateOutputs.isPending || !!job} onClick={() => generateOutputs.mutate()}><Play size={18} />Generate DBML & Trace</Button>
	) : (
		<Button variant="primary" onClick={() => navigate(finalizePath(projectId, project.data?.project.lifecycle_status))}>{project.data?.project.lifecycle_status === "completed" ? "Open Completed" : "Open Finalize"}<ArrowRight size={18} /></Button>
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
		{job && <Panel title="Generating final outputs"><JobProgress projectId={projectId} job={job} onDone={() => { setJob(null); void queryClient.invalidateQueries().then(() => navigate(`/projects/${projectId}/dbml`)); }} /></Panel>}
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
	const { navigate } = useRouter();
	const queryClient = useQueryClient();
	const project = useQuery({ queryKey: ["project", projectId], queryFn: () => api.getProject(projectId) });
	const conceptual = useQuery({ queryKey: ["conceptual-model", projectId], queryFn: () => api.conceptualModel(projectId), retry: false });
	const accept = useMutation({
		mutationFn: () => api.acceptConceptualModel(projectId, project.data?.project.current_revision ?? 0),
		onSuccess: async () => {
			await queryClient.invalidateQueries();
			requestAutoRun(projectId);
			navigate(`/projects/${projectId}/analysis/overview`);
		},
	});
	// Regeneration runs the conceptual LLM step again from the same segments,
	// e.g. after a prompt change; it replaces the proposal and any later model.
	const [regenerateJob, setRegenerateJob] = useState<Job | null>(null);
	const regenerate = useMutation({
		mutationFn: async () => {
			const fresh = await queryClient.fetchQuery({ queryKey: ["project", projectId], queryFn: () => api.getProject(projectId), staleTime: 0 });
			return api.runStage(projectId, "conceptual_model", fresh.project.current_revision);
		},
		onSuccess: ({ job: started }) => setRegenerateJob(started),
	});
	if (regenerateJob) {
		return (
			<Panel title="Regenerating the conceptual model">
				<JobProgress projectId={projectId} job={regenerateJob} onDone={() => { setRegenerateJob(null); void queryClient.invalidateQueries(); }} onDismiss={() => setRegenerateJob(null)} />
			</Panel>
		);
	}
	if (conceptual.isLoading) return <LoadingState />;
	if (conceptual.isError) return <Panel title="Conceptual model is not ready"><p className="muted">Run the conceptual model stage from the pipeline first.</p></Panel>;
	const model = normalizeConceptualModelForView(conceptual.data!.conceptual_model);
	const labelOf = (id: string) => model.entity_concepts.find((entity) => entity.id === id)?.label ?? id;
	const extras = [
		["Lifecycles", model.lifecycle_concepts],
		["Derived", model.derived_concepts],
		["Files", model.file_concepts],
		["Imports", model.import_concepts],
	] as const;
	return (
		<div className="page">
			<Panel title="Conceptual decision gate" action={
				<div className="toolbar">
					<Button disabled={regenerate.isPending} onClick={() => regenerate.mutate()}><RotateCcw size={16} />Regenerate</Button>
					{!conceptual.data?.accepted ? <Button variant="primary" disabled={accept.isPending || !conceptual.data?.qa.ok} onClick={() => accept.mutate()}><CheckCircle2 size={18} />Accept Conceptual Model</Button> : <StatusBadge value="accepted" />}
				</div>
			}>
				{regenerate.isError && <p className="error-text">{regenerate.error instanceof Error ? regenerate.error.message : "Regeneration could not be started."}</p>}
				<div className="toolbar">
					<Badge tone="good">{model.entity_concepts.length} entities</Badge>
					<Badge>{model.relationships.length} relationships</Badge>
					<Badge>{model.entity_concepts.reduce((sum, entity) => sum + entity.attributes.length, 0)} attributes</Badge>
					<Badge tone={conceptual.data?.qa.ok ? "good" : "bad"}>{conceptual.data?.qa.ok ? "QA passed" : "QA errors"}</Badge>
					{conceptual.data?.qa.coverage?.description_segments !== undefined && (
						<Badge tone={conceptual.data.qa.coverage.description_uncovered_segments ? "warn" : "good"}>
							{conceptual.data.qa.coverage.description_covered_segments}/{conceptual.data.qa.coverage.description_segments} segments covered
						</Badge>
					)}
				</div>
				<p className="muted">Review identities, attributes and cardinalities. After acceptance the model is projected into DB-DSL and validated automatically.</p>
				{!conceptual.data?.qa.ok && <ul className="plain-list error-text">{conceptual.data?.qa.errors.map((error) => <li key={error}>{error}</li>)}</ul>}
				{accept.isError && <p className="error-text">{accept.error instanceof Error ? accept.error.message : "Acceptance failed."}</p>}
			</Panel>
			<Panel title="Conceptual ER diagram">
				<ConceptualDiagram projectId={projectId} model={model} />
			</Panel>
			{conceptual.data?.description && <DescriptionPanel description={conceptual.data.description} warnings={conceptual.data.qa.warnings} />}
			<div className="grid-2">
				<Panel title={`Entities · ${model.entity_concepts.length}`}>
					<div className="field" style={{ gap: 10 }}>
						{model.entity_concepts.map((entity) => (
							<div className="review-option-card" key={entity.id}>
								<div className="toolbar"><strong>{entity.label}</strong><Badge>{entity.kind}</Badge></div>
								{entity.description && <p className="muted">{entity.description}</p>}
								<div className="attribute-chips">
									{entity.attributes.map((attribute) => (
										<span className={`attribute-chip ${attribute.required ? "required" : ""}`} key={attribute.id} title={attribute.description}>
											{attribute.label}{attribute.required ? " *" : ""}
											{attribute.value_type && <span className="chip-type">{attribute.value_type}{attribute.unique ? " · unique" : ""}</span>}
										</span>
									))}
									{entity.attributes.length === 0 && <span className="muted">no attributes</span>}
								</div>
								<div className="toolbar evidence-row">{entity.evidence.source_units.map((id) => <Badge key={id}>{id}</Badge>)}</div>
							</div>
						))}
					</div>
				</Panel>
				<Panel title={`Relationships · ${model.relationships.length}`}>
					<div className="field" style={{ gap: 10 }}>
						{model.relationships.map((relationship) => (
							<div className="review-option-card" key={relationship.id}>
								<div className="toolbar"><strong>{labelOf(relationship.from)}</strong><Badge tone="good">{cardinalityLabels[relationship.cardinality] ?? relationship.cardinality}</Badge><strong>{labelOf(relationship.to)}</strong>{relationship.required !== undefined && <Badge>{relationship.required ? "mandatory" : "optional"}</Badge>}</div>
								<p>{relationship.label}</p>
								{relationship.description && <p className="muted">{relationship.description}</p>}
							</div>
						))}
						{model.relationships.length === 0 && <p className="muted">No conceptual relationships proposed.</p>}
					</div>
					{extras.filter(([, items]) => items.length > 0).map(([title, items]) => (
						<div key={title} style={{ marginTop: 14 }}>
							<strong>{title}</strong>
							<ul className="plain-list">{items.map((item) => <li key={item.id}><strong>{item.label}</strong>{item.description ? <span className="muted"> — {item.description}</span> : null}</li>)}</ul>
						</div>
					))}
					{model.unresolved_review_ids.length > 0 && <p className="error-text">Unresolved: {model.unresolved_review_ids.join(", ")}</p>}
				</Panel>
			</div>
		</div>
	);
}

// DescriptionPanel shows what the conceptual model was derived from and what it
// does not contain: actors, rules, open questions, boundaries and the segments
// the LLM left out of the model, with the reasons it gave.
function DescriptionPanel({ description, warnings }: { description: ConceptualDescription; warnings: string[] }) {
	const evidence = (segments: string[]) => segments.length > 0 && <span className="muted"> · {segments.join(", ")}</span>;
	return (
		<>
			{(description.open_questions.length > 0 || warnings.length > 0) && (
				<Panel title={`Open questions · ${description.open_questions.length}`}>
					<ul className="plain-list">
						{description.open_questions.map((question) => (
							<li key={question.id}>
								<strong>{question.question}</strong>{evidence(question.evidence.segments)}
								{question.readings.length > 0 && <div className="muted">{question.readings.join(" · ")}</div>}
							</li>
						))}
					</ul>
					{warnings.length > 0 && <ul className="plain-list muted">{warnings.map((warning) => <li key={warning}>{warning}</li>)}</ul>}
				</Panel>
			)}
			<div className="grid-2">
				<Panel title={`Actors · ${description.actors.length}`}>
					<ul className="plain-list">
						{description.actors.map((actor) => (
							<li key={actor.id}><strong>{actor.name}</strong>{actor.description ? <span className="muted"> — {actor.description}</span> : null}{evidence(actor.evidence.segments)}</li>
						))}
					</ul>
				</Panel>
				<Panel title={`Rules · ${description.rules.length}`}>
					<ul className="plain-list">
						{description.rules.map((rule) => (
							<li key={rule.id}><Badge>{rule.kind}</Badge> {rule.statement}{evidence(rule.evidence.segments)}</li>
						))}
					</ul>
				</Panel>
			</div>
			{(description.boundaries.length > 0 || description.excluded.length > 0) && (
				<div className="grid-2">
					<Panel title={`Boundaries · ${description.boundaries.length}`}>
						<ul className="plain-list">
							{description.boundaries.map((boundary, index) => (
								<li key={index}><Badge>{boundary.kind}</Badge> {boundary.description}{boundary.kept_outcome ? <span className="muted"> — kept: {boundary.kept_outcome}</span> : null}</li>
							))}
						</ul>
					</Panel>
					<Panel title={`Not in the model · ${description.excluded.length} segments`}>
						<div className="toolbar">
							{description.excluded.map((item) => <Badge key={item.segment} tone="default">{item.segment} · {item.reason}</Badge>)}
						</div>
					</Panel>
				</div>
			)}
		</>
	);
}

const conceptNodeTypes = { conceptNode: memo(ConceptNode) };

function ConceptNode({ data }: NodeProps<{ label: string; attributes: { id: string; label: string; required: boolean; value_type?: string }[] }>) {
	return (
		<div className="table-node concept-node">
			<Handle type="target" position={Position.Left} />
			<div className="table-node-title">{data.label}</div>
			{data.attributes.slice(0, visibleFields).map((attribute) => (
				<div className="field-row" key={attribute.id}>
					<span className="truncate">{attribute.label}</span>
					<span className="muted">{attribute.value_type ?? ""}{attribute.required ? " *" : ""}</span>
				</div>
			))}
			{data.attributes.length > visibleFields && <div className="field-row muted">+ {data.attributes.length - visibleFields} more</div>}
			<Handle type="source" position={Position.Right} />
		</div>
	);
}

function ConceptualDiagram({ projectId, model }: { projectId: string; model: ReturnType<typeof normalizeConceptualModelForView> }) {
	const entities = model.entity_concepts;
	const heightOf = (count: number) => 40 + 28 * Math.min(count, visibleFields) + (count > visibleFields ? 28 : 0);
	const layout = useQuery({
		queryKey: ["conceptual-layout", projectId, entities.map((entity) => `${entity.id}:${entity.attributes.length}`).join(","), model.relationships.length],
		queryFn: () => layoutGraph(
			entities.map((entity) => ({ id: entity.id, width: tableNodeWidth, height: heightOf(entity.attributes.length) })),
			model.relationships,
		),
		enabled: entities.length > 0,
		staleTime: Infinity,
	});
	const nodes: Node[] = entities.map((entity, index) => ({
		id: entity.id,
		type: "conceptNode",
		position: layout.data?.[entity.id] ?? { x: (index % 4) * 300, y: Math.floor(index / 4) * 280 },
		data: { label: entity.label, attributes: entity.attributes },
		style: { width: tableNodeWidth },
	}));
	const edges: Edge[] = model.relationships.map((relationship) => ({
		id: relationship.id,
		source: relationship.from,
		target: relationship.to,
		type: "smoothstep",
		label: `${relationship.label} · ${cardinalityLabels[relationship.cardinality] ?? relationship.cardinality}`,
		labelBgPadding: [6, 3] as [number, number],
		labelBgBorderRadius: 4,
		labelStyle: { fontSize: 11, fontWeight: 600 },
		markerEnd: { type: MarkerType.ArrowClosed },
		style: { stroke: "#64748b", strokeWidth: 1.5 },
	}));
	return (
		<div className="diagram-pane">
			<ReactFlow key={layout.data ? "laid-out" : "grid"} nodes={nodes} edges={edges} nodeTypes={conceptNodeTypes} fitView fitViewOptions={{ padding: 0.04 }} minZoom={0.2}>
				<Background />
				<Controls />
			</ReactFlow>
		</div>
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
	const layoutNodes = graph.data?.model_graph.nodes ?? [];
	const layoutEdges = graph.data?.model_graph.edges ?? [];
	const layout = useQuery({
		queryKey: ["model-layout", projectId, layoutNodes.map((node) => `${node.id}:${node.fields?.length ?? 0}`).join(","), layoutEdges.length],
		queryFn: () => layoutGraph(layoutNodes.map((node) => ({ id: node.id, width: tableNodeWidth, height: tableNodeHeight(node) })), layoutEdges),
		enabled: layoutNodes.length > 0,
		staleTime: Infinity,
	});
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
            key={layout.data ? "laid-out" : "grid"}
            nodes={toFlowNodes(graphNodes, [...highlightedElementsSet], selectedElementID, layout.data)}
            edges={toFlowEdges(graphEdges, [...highlightedElementsSet])}
            nodeTypes={nodeTypes}
            onNodeClick={(_, node) => handleElementSelect(node.id)}
            onEdgeClick={(_, edge) => handleElementSelect(edge.id)}
            fitView
            fitViewOptions={{ padding: 0.04 }}
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
      {(model.fields ?? []).slice(0, visibleFields).map((field) => (
        <div className={`field-row ${highlighted.includes(field.element_id) ? "highlighted" : ""}`} key={field.element_id}>
          <span className="truncate">{field.id}</span>
          <span className="muted">{field.type}</span>
        </div>
      ))}
      {(model.fields?.length ?? 0) > visibleFields && <div className="field-row muted">+ {(model.fields?.length ?? 0) - visibleFields} more</div>}
      <Handle type="source" position={Position.Right} />
    </div>
  );
}

const tableNodeWidth = 230;
const visibleFields = 9;

function tableNodeHeight(model: ModelNode) {
	const fields = model.fields?.length ?? 0;
	return 40 + 28 * Math.min(fields, visibleFields) + (fields > visibleFields ? 28 : 0);
}

function toFlowNodes(models: ModelNode[], highlighted: string[], selected: string | null, positions?: Record<string, { x: number; y: number }>): Node[] {
  const columns = 4;
  return models.map((model, index) => ({
    id: model.id,
    type: "tableNode",
    position: positions?.[model.id] ?? { x: (index % columns) * 300, y: Math.floor(index / columns) * 280 },
    data: { model, highlighted },
    selected: selected === model.id,
    draggable: true,
    selectable: true,
    style: { width: tableNodeWidth },
  })) as Node[];
}

const cardinalityLabels: Record<string, string> = { one_to_one: "1 : 1", one_to_many: "1 : N", many_to_one: "N : 1", many_to_many: "M : N" };

function toFlowEdges(models: { id: string; from: string; to: string; cardinality: string }[], highlighted: string[]): Edge[] {
  return models.map((edge) => ({
    id: edge.id,
    source: edge.from,
    target: edge.to,
    label: cardinalityLabels[edge.cardinality] ?? edge.cardinality,
    type: "smoothstep",
    labelBgPadding: [6, 3] as [number, number],
    labelBgBorderRadius: 4,
    labelStyle: { fontSize: 11, fontWeight: 600 },
    markerEnd: { type: MarkerType.ArrowClosed },
    animated: highlighted.includes(edge.id),
    style: { stroke: highlighted.includes(edge.id) ? "#0f766e" : "#94a3b8", strokeWidth: highlighted.includes(edge.id) ? 2.5 : 1.5 },
  }));
}

function QualityView({ projectId }: { projectId: string }) {
  const { navigate } = useRouter();
  const queryClient = useQueryClient();
  const project = useQuery({ queryKey: ["project", projectId], queryFn: () => api.getProject(projectId) });
  const quality = useQuery({ queryKey: ["quality", projectId], queryFn: () => api.quality(projectId) });
	const semantic = useQuery({ queryKey: ["semantic-verification", projectId], queryFn: () => api.semanticVerification(projectId), retry: false });
	const mappingReport = useQuery({ queryKey: ["logical-mapping-report", projectId], queryFn: () => api.logicalMappingReport(projectId), retry: false });
	const createSemanticRepairs = useMutation({
		mutationFn: () => api.createSemanticRepairCandidates(projectId, project.data?.project.current_revision ?? 0),
		onSuccess: () => {
			void queryClient.invalidateQueries();
			navigate(`/projects/${projectId}/analysis/review`);
		},
	});
  const accept = useMutation({
    mutationFn: (id: string) => api.acceptQuality(projectId, id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["quality", projectId] }),
  });

  if (quality.isLoading) return <LoadingState />;
  const report = quality.data?.quality;
  const mapping = mappingReport.data?.logical_mapping_report;
  return (
    <div className="page">
      <div className="grid-3">
        <MetricWithIcon label="Validation errors" value={report?.summary.validation_errors ?? 0} bad={(report?.summary.validation_errors ?? 0) > 0} />
        <MetricWithIcon label="Lint warnings" value={report?.summary.lint_warnings ?? 0} bad={false} />
        <MetricWithIcon label="Blocking issues" value={report?.summary.blocking_issues ?? 0} bad={(report?.summary.blocking_issues ?? 0) > 0} />
      </div>
		{mapping && mapping.strategy === "deterministic" && (
			<Panel title="Logical mapping (deterministic rules)" action={<Badge tone="good">{mapping.rule_version}</Badge>}>
				<div className="toolbar">
					<Badge>{mapping.entities} tables</Badge><Badge>{mapping.relationships} relationships</Badge><Badge>{mapping.constraints} constraints</Badge>
					<Badge>{mapping.state_machines} state machines</Badge><Badge>{mapping.derived_views} derived views</Badge>
					<Badge tone={(mapping.inferred?.length ?? 0) > 0 ? "warn" : "good"}>{mapping.inferred?.length ?? 0} inferred values</Badge>
				</div>
				<p className="muted">The accepted conceptual model was projected into DB-DSL by fixed, versioned rules without an LLM. Every rule application is listed below.</p>
				{(mapping.decisions?.length ?? 0) > 0 && <details open><summary><strong>Applied rules · {mapping.decisions!.length}</strong></summary><ul className="plain-list">{mapping.decisions!.map((line) => <li key={line}>{line}</li>)}</ul></details>}
				{(mapping.inferred?.length ?? 0) > 0 && <details><summary><strong>Inferred values · {mapping.inferred!.length}</strong></summary><ul className="plain-list">{mapping.inferred!.map((line) => <li key={line}>{line}</li>)}</ul></details>}
				{(mapping.warnings?.length ?? 0) > 0 && <details><summary><strong>Not representable in DDL · {mapping.warnings!.length}</strong></summary><ul className="plain-list">{mapping.warnings!.map((line) => <li key={line}>{line}</li>)}</ul></details>}
			</Panel>
		)}
		{semantic.data?.semantic_verification && (
			<Panel
				title="Semantic obligation gate"
				action={semantic.data.semantic_verification.blocking_issues > 0 ? (
					<Button
						variant="primary"
						disabled={createSemanticRepairs.isPending || project.isLoading}
						onClick={() => createSemanticRepairs.mutate()}
					>
						<GitBranch size={16} /> Resolve blocking obligations
					</Button>
				) : undefined}
			>
				<div className="toolbar">
					<Badge tone={semantic.data.semantic_verification.ok ? "good" : "bad"}>{semantic.data.semantic_verification.ok ? "Passed" : "Blocked"}</Badge>
					<Badge>{semantic.data.semantic_verification.obligations_realized}/{semantic.data.semantic_verification.obligations_required} required obligations realized</Badge>
					<Badge tone={semantic.data.semantic_verification.blocking_issues ? "bad" : "good"}>{semantic.data.semantic_verification.blocking_issues} blocking issues</Badge>
				</div>
				{createSemanticRepairs.isError && <p className="error-text">{createSemanticRepairs.error instanceof Error ? createSemanticRepairs.error.message : "Semantic repair queue could not be created."}</p>}
				{(semantic.data.semantic_verification.issues ?? []).map((issue) => (
					<div className="review-option-card" key={issue.id}>
						<div className="toolbar">
							<strong>{issue.id}</strong>
							{issue.obligation_id && <Badge>{issue.obligation_id}</Badge>}
							<StatusBadge value={issue.severity} />
							<Badge tone={issue.blocking ? "bad" : "warn"}>{issue.blocking ? "blocking" : "non-blocking"}</Badge>
						</div>
						<p>{issue.message}</p>
						<p className="muted">{issue.code}</p>
						{(issue.model_elements?.length ?? 0) > 0 && <p className="muted">Current evidence: {issue.model_elements?.join(", ")}</p>}
					</div>
				))}
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
