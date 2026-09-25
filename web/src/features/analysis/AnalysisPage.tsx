import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertCircle, CheckCircle2, Circle, GitBranch, Loader2, Play, XCircle } from "lucide-react";
import { api } from "@/shared/api/client";
import type {
	ArtifactHealth,
  CrudOperation,
	CombinedDocumentSentence,
  FunctionalArea,
  InputResource,
  Job,
  RequirementAtom,
  ReviewCandidate,
  SourceUnit,
  StructuredExample,
	ProjectStageName,
	SourceSegmentationProposal,
	SourceUnitQA,
} from "@/shared/api/types";
import { Badge, Button, Drawer, Field, LoadingState, Metric, Panel, StatusBadge } from "@/shared/components/ui";
import { humanizeStatus } from "@/shared/lib/status";
import { useRouter } from "@/shared/lib/router";
import { JobProgress, friendlyError } from "@/features/jobs/JobProgress";
import { PipelineStepper } from "./PipelineStepper";
import { SourceTraceGraph } from "./SourceTraceGraph";
	import { isRunnableStage, nextStageLabel, shouldRecoverLatestJob, unresolvedReviewDependencies } from "@/shared/lib/pipeline";
import { consumeAutoRun, gatePath } from "@/shared/lib/autopilot";

const tabs = [
	["overview", "Overview"],
  ["sources", "Sources & Examples"],
  ["requirements", "Requirements"],
  ["functional-crud", "Functional / CRUD"],
	["review", "Review Queue"],
	["activity", "Activity & LLM Runs"],
] as const;

// Requirements, functional/CRUD analysis and review questions are not produced
// by the segment-based flow; their tabs appear only for projects that have them.
function tabVisible(id: string, health: ArtifactHealth | undefined) {
	if (health?.segment_flow && (id === "requirements" || id === "functional-crud" || id === "review")) return false;
	if (id === "requirements") return health?.requirement_atoms_status === "ready";
	if (id === "functional-crud") return health?.functional_analysis_status === "ready";
	if (id === "review") return health?.review_candidates_status === "ready";
	return true;
}

export function AnalysisPage({ projectId, mode }: { projectId: string; mode: string }) {
  const { navigate } = useRouter();
	const queryClient = useQueryClient();
  const project = useQuery({ queryKey: ["project", projectId], queryFn: () => api.getProject(projectId) });
  const summary = useQuery({ queryKey: ["analysis-summary", projectId], queryFn: () => api.analysisSummary(projectId) });
	const stages = useQuery({ queryKey: ["project-stages", projectId], queryFn: () => api.stageStatus(projectId) });
	const jobs = useQuery({ queryKey: ["jobs", projectId], queryFn: () => api.jobs(projectId), retry: false, refetchInterval: 2000 });
  const [job, setJob] = useState<Job | null>(null);
	const [dismissedJobIds, setDismissedJobIds] = useState<Set<string>>(() => new Set());
	const [autoRun, setAutoRun] = useState(() => consumeAutoRun(projectId));
	const runNext = useMutation({
		mutationFn: ({ stage, revision }: { stage: ProjectStageName; revision: number }) => {
			return stage === "combined_document" || stage === "process_sources"
				? api.processSources(projectId, revision)
				: api.runStage(projectId, stage, revision);
		},
		onSuccess: ({ job: started }) => {
			setDismissedJobIds((current) => {
				const next = new Set(current);
				next.delete(started.id);
				return next;
			});
			setJob(started);
		},
		onError: () => setAutoRun(false),
  });
	// Reads fresh stage status after a job instead of trusting cached queries,
	// so the autopilot never restarts the stage that just finished.
	const advance = async (completedStage?: string) => {
		const [status, fresh] = await Promise.all([
			queryClient.fetchQuery({ queryKey: ["project-stages", projectId], queryFn: () => api.stageStatus(projectId), staleTime: 0 }),
			queryClient.fetchQuery({ queryKey: ["project", projectId], queryFn: () => api.getProject(projectId), staleTime: 0 }),
		]);
		if (isRunnableStage(status.next_stage) && status.next_stage !== completedStage) {
			runNext.mutate({ stage: status.next_stage, revision: fresh.project.current_revision });
			return;
		}
		setAutoRun(false);
		const path = gatePath(projectId, status.next_stage, fresh.project.lifecycle_status);
		if (path) navigate(path);
	};
	// StrictMode runs mount effects twice in development; the ref keeps the
	// hand-over from starting two jobs.
	const handedOver = useRef(false);
	useEffect(() => {
		if (handedOver.current) return;
		handedOver.current = true;
		if (autoRun && !job && !runNext.isPending) void advance();
		// Only the initial hand-over from another page starts here; later steps are driven by job completion.
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, []);
	const latestJob = jobs.data?.items[0];

	useEffect(() => {
		const currentRevision = project.data?.project.current_revision;
		if (job && currentRevision !== undefined) {
			if (!shouldRecoverLatestJob(job, currentRevision)) setJob(null);
			return;
		}
		if (
			!latestJob ||
			currentRevision === undefined ||
			dismissedJobIds.has(latestJob.id) ||
			!shouldRecoverLatestJob(latestJob, currentRevision)
		) return;
		setJob(latestJob);
	}, [dismissedJobIds, job, latestJob, project.data?.project.current_revision]);

  const openReviews = project.data?.project.counts.open_review_questions ?? 0;
	const health = stages.data?.artifact_health ?? project.data?.artifact_health;
	const nextStage = stages.data?.next_stage;
	const nextAction = nextStageLabel(nextStage, openReviews);
	const handleNext = () => {
		if (isRunnableStage(nextStage)) {
			setAutoRun(true);
			runNext.mutate({ stage: nextStage, revision: project.data?.project.current_revision ?? 0 });
			return;
		}
		const path = gatePath(projectId, nextStage, project.data?.project.lifecycle_status);
		if (path) navigate(path);
	};
	const onStageDone = async (completedStage: string) => {
		setJob(null);
		await queryClient.invalidateQueries();
		if (autoRun) await advance(completedStage);
		else if (completedStage === "process_sources" || completedStage === "combined_document") navigate(`/projects/${projectId}/analysis/sources`);
	};

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1 className="page-title">{project.data?.project.name ?? "Project"} | Analysis Workspace</h1>
          <p className="page-subtitle">
            {project.data?.project.counts.source_units ?? 0} source units · conceptual model {humanizeStatus(health?.conceptual_model_status ?? "not_generated").toLowerCase()} · logical model {humanizeStatus(health?.model_status ?? "not_generated").toLowerCase()}
          </p>
        </div>
        <div className="toolbar">
			<Button variant="primary" disabled={!nextStage || runNext.isPending || !!job} onClick={handleNext}>
				{openReviews > 0 ? <AlertCircle size={18} /> : <Play size={18} />}
				{nextAction}
			</Button>
        </div>
      </div>

		{health && <PipelineStepper health={health} />}

      {job && (
		<Panel title={stageLabel(job.type)}>
			<JobProgress
				projectId={projectId}
				job={job}
				onDone={() => void onStageDone(job.stage)}
				onDismiss={(dismissed) => {
					setDismissedJobIds((current) => new Set(current).add(dismissed.id));
					setJob(null);
					setAutoRun(false);
				}}
			/>
        </Panel>
      )}
		{runNext.isError && <p className="error-text">{runNext.error instanceof Error ? runNext.error.message : "Stage could not be started."}</p>}

      <div className="grid-3">
        <Metric label="Source units" value={project.data?.project.counts.source_units ?? "-"} />
        <Metric label="Conceptual model" value={humanizeStatus(health?.conceptual_model_status ?? "not_generated")} />
        <Metric label="Logical model" value={humanizeStatus(health?.model_status ?? "not_generated")} />
      </div>

      <div className="tabs">
        {tabs.filter(([id]) => tabVisible(id, health)).map(([id, label]) => (
          <button
            className={`tab ${mode === id ? "active" : ""}`}
            key={id}
            onClick={() => navigate(`/projects/${projectId}/analysis/${id}`)}
          >
            {label}
          </button>
        ))}
      </div>

		{project.isLoading || summary.isLoading || stages.isLoading ? (
        <LoadingState />
		) : mode === "overview" ? (
			<ProjectOverview projectId={projectId} nextAction={nextAction} onNext={handleNext} disabled={!nextStage || !!job || runNext.isPending} />
		) : mode === "activity" ? (
			<ActivityView projectId={projectId} />
		) : mode === "requirements" ? (
        <RequirementsTab projectId={projectId} />
      ) : mode === "functional-crud" ? (
        <FunctionalCrudTab projectId={projectId} />
      ) : mode === "review" ? (
		<ReviewTab
			projectId={projectId}
			revision={project.data?.project.current_revision ?? 0}
			nextAction={nextAction}
			onNext={handleNext}
			onDecisionsApplied={() => { setAutoRun(true); void advance(); }}
			nextDisabled={!nextStage || !!job || runNext.isPending}
		/>
      ) : (
        <SourcesTab projectId={projectId} />
      )}
    </div>
  );
}

function ActivityView({ projectId }: { projectId: string }) {
	const jobs = useQuery({ queryKey: ["jobs", projectId], queryFn: () => api.jobs(projectId), retry: false });
	const runs = useQuery({ queryKey: ["llm-runs", projectId], queryFn: () => api.llmRuns(projectId), retry: false });
	const optimization = useQuery({ queryKey: ["optimization-report", projectId], queryFn: () => api.optimizationReport(projectId), retry: false });
	if (jobs.isLoading || runs.isLoading || optimization.isLoading) return <LoadingState />;
	const metrics = optimization.data?.report;
	return (
		<div className="activity-stack">
			<Panel title="LLM optimization report">
				<div className="grid-3">
					<Metric label="Provider calls" value={metrics?.totals.provider_calls ?? 0} />
					<Metric label="Cache hits" value={metrics?.totals.cache_hits ?? 0} />
					<Metric label="Retries" value={metrics?.totals.retries ?? 0} />
					<Metric label="Tokens" value={(metrics?.totals.total_tokens ?? 0).toLocaleString("en-US")} />
					<Metric label="Context saved" value={`${((metrics?.totals.context_reduction_ratio ?? 0) * 100).toFixed(1)}%`} />
					<Metric label="Review calls avoided" value={metrics?.avoided_review_resolution_calls ?? 0} />
					<Metric label="Generation calls avoided" value={metrics?.avoided_generation_calls ?? 0} />
					<Metric label="Usage unknown" value={metrics?.totals.unknown_usage_attempts ?? 0} />
				</div>
				<p className="muted">Policies: {metrics?.budget_policy ?? "-"} · {metrics?.context_policy ?? "-"} · {metrics?.call_gate_policy ?? "-"}</p>
			</Panel>
			<Panel title={`Jobs · ${jobs.data?.items.length ?? 0}`}>
				<table className="data-table"><thead><tr><th>Stage</th><th style={{ width: 110 }}>Status</th><th style={{ width: 90 }}>Revision</th><th style={{ width: 80 }}>Progress</th></tr></thead><tbody>
					{(jobs.data?.items ?? []).map((job) => <tr key={job.id}><td><strong>{stageLabel(job.stage)}</strong><div className="muted">{job.id}</div></td><td><StatusBadge value={job.status} />{job.error && <div className="error-text small">{friendlyError(job.error)}</div>}</td><td>{job.input_revision || "-"} → {job.output_revision || "-"}</td><td>{job.progress}%</td></tr>)}
				</tbody></table>
				{!jobs.data?.items.length && <p className="muted">No jobs recorded.</p>}
			</Panel>
			<Panel title={`Sanitized LLM runs · ${runs.data?.items.length ?? 0}`}>
				<table className="data-table"><thead><tr><th>Stage / model</th><th>Status</th><th>Duration</th><th>Tokens</th></tr></thead><tbody>
					{(runs.data?.items ?? []).map((run) => <tr key={run.id}><td><strong>{stageLabel(run.stage)}</strong><div className="muted">{run.provider} · {run.model}</div><div className="muted">validation: {run.validation_scope ?? "structured schema"}</div></td><td><StatusBadge value={run.status} />{(run.errors?.length ?? 0) > 0 && <div className="error-text">{run.errors!.length} issue(s)</div>}</td><td>{formatDuration(run.duration_ms ?? 0)}</td><td>{run.usage.total_tokens ? run.usage.total_tokens.toLocaleString("en-US") : "-"}</td></tr>)}
				</tbody></table>
				{!runs.data?.items.length && <p className="muted">No LLM runs recorded.</p>}
			</Panel>
		</div>
	);
}

function ProjectOverview({ projectId, nextAction, onNext, disabled }: { projectId: string; nextAction: string; onNext: () => void; disabled: boolean }) {
	const jobs = useQuery({ queryKey: ["jobs", projectId], queryFn: () => api.jobs(projectId), retry: false, refetchInterval: 2000 });
	const latest = jobs.data?.items[0];
	return (
		<div className="grid-2">
			<Panel title="Recommended next action" action={<Button variant="primary" onClick={onNext} disabled={disabled}>{nextAction}</Button>}>
				<p>Continue from the first incomplete gate. Accepted upstream artifacts stay versioned and unchanged if the stage fails.</p>
			</Panel>
			<Panel title="Recent activity">
				{latest ? (
					<div><div className="toolbar"><StatusBadge value={latest.status} /><strong>{stageLabel(latest.stage)}</strong><span className="muted">{latest.id} · revision {latest.input_revision || "-"} · {latest.progress}%</span></div>{(latest.error || latest.message) && <p className={latest.error ? "error-text" : "muted"}>{latest.error ? friendlyError(latest.error) : latest.message}</p>}</div>
				) : <p className="muted">No pipeline jobs have been recorded yet.</p>}
			</Panel>
		</div>
	);
}

function formatDuration(ms: number) {
	if (ms < 1000) return `${ms} ms`;
	const seconds = ms / 1000;
	return seconds < 60 ? `${seconds.toFixed(1)} s` : `${Math.floor(seconds / 60)} min ${Math.round(seconds % 60)} s`;
}

function stageLabel(stage: string) {
	return stage.replace(/_/g, " ").replace(/\b\w/g, (letter: string) => letter.toUpperCase());
}

type StripState = "done" | "warn" | "running" | "pending" | "failed";

interface StripStep {
	title: string;
	actor: "code" | "LLM" | "you";
	state: StripState;
	value: string;
	detail: string;
	target?: string;
}

// SourceStatusStrip shows the live state of every source step in one row and
// who performs it, so the division of work between LLM, code and person is visible.
function SourceStatusStrip({ steps }: { steps: StripStep[] }) {
	const icons: Record<StripState, typeof CheckCircle2> = { done: CheckCircle2, warn: AlertCircle, running: Loader2, pending: Circle, failed: XCircle };
	return (
		<nav className="source-strip" aria-label="Source processing status">
			{steps.map((step) => {
				const Icon = icons[step.state];
				return (
					<button
						key={step.title}
						className={`source-strip-step ${step.state}`}
						disabled={!step.target}
						onClick={() => step.target && document.getElementById(step.target)?.scrollIntoView({ behavior: "smooth", block: "start" })}
					>
						<span className="source-strip-top">
							<Icon size={15} className={step.state === "running" ? "spin" : undefined} />
							<strong>{step.title}</strong>
							<span className={`actor-chip ${step.actor.toLowerCase()}`}>{step.actor}</span>
						</span>
						<span className="source-strip-value">{step.value}</span>
						<span className="source-strip-detail">{step.detail}</span>
					</button>
				);
			})}
		</nav>
	);
}

function CombinedSentenceList({ items }: { items: CombinedDocumentSentence[] }) {
	if (!items.length) return <p className="muted">No evidence segments generated.</p>;
	return (
		<div className="od-sentence-list">
			{items.map((item) => (
				<article className="od-sentence-card" key={item.id}>
					<div className="toolbar">
						<strong>{item.id}</strong>
						<Badge>{item.kind ?? "sentence"}</Badge>
						{item.role && <Badge>{item.role}</Badge>}
						{item.transformation && <Badge tone={item.transformation === "merged" ? "warn" : "default"}>{item.transformation}</Badge>}
						{item.confidence && <Badge tone={item.confidence === "low" ? "warn" : "good"}>{item.confidence}</Badge>}
					</div>
					<p>{item.text}</p>
					{(item.derived_from?.length ?? 0) > 0 && <div className="muted">{item.derived_from?.map((origin) => `${origin.resource_id} · lines ${origin.line_start}–${origin.line_end}${origin.source_segment_id ? ` · ${origin.source_segment_id} bytes ${origin.start_byte ?? 0}–${origin.end_byte ?? 0}` : ""}`).join(", ")}</div>}
					{(item.warnings?.length ?? 0) > 0 && <div className="error-text">{item.warnings?.join(" ")}</div>}
				</article>
			))}
		</div>
	);
}

function SegmentationList({ proposal }: { proposal: SourceSegmentationProposal }) {
	if (!proposal.segments.length) return <p className="muted">No segments generated.</p>;
	return (
		<div className="od-sentence-list">
			{proposal.segments.map((segment) => (
				<article className="od-sentence-card" key={segment.id}>
					<div className="toolbar">
						<strong>{segment.id}</strong>
						<Badge>{segment.type}</Badge>
					</div>
					<p>{segment.text}</p>
				</article>
			))}
		</div>
	);
}

function ReviewAuditSummary({ decisions }: { decisions: NonNullable<SourceUnitQA["review_decisions"]> }) {
	if (!decisions.length) return <p className="muted">No source-unit review decisions have been recorded.</p>;
	return (
		<div className="review-audit-list">
			{decisions.slice().reverse().map((decision) => (
				<article className="review-audit-item" key={`${decision.source_unit_id}-${decision.project_revision}`}>
					<div className="toolbar">
						<strong>{decision.source_unit_id}</strong>
						<StatusBadge value={decision.decision} />
						<Badge>rev {decision.project_revision}</Badge>
					</div>
					<div className="muted">{decision.reviewed_by} · {new Date(decision.reviewed_at).toLocaleString()}</div>
					{decision.note && <p>{decision.note}</p>}
					{decision.normalization ? (
						<>
							<div className="toolbar">
								<Badge>{decision.normalization.version || "legacy"}</Badge>
								<Badge tone={decision.normalization.changed ? "warn" : "good"}>{decision.normalization.changed ? "Changed" : "Unchanged"}</Badge>
								<span className="muted">{decision.normalization.operations.length} operations</span>
							</div>
							<div className="muted">{shortAuditHash(decision.normalization.exact_hash)} → {shortAuditHash(decision.normalization.normalized_hash)}</div>
						</>
					) : <div className="muted">Normalization audit unavailable for this legacy decision.</div>}
				</article>
			))}
		</div>
	);
}

function SourcesTab({ projectId }: { projectId: string }) {
	const queryClient = useQueryClient();
  const [filter, setFilter] = useState("all");
	const [reviewFilterSelected, setReviewFilterSelected] = useState(false);
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<SourceUnit | null>(null);
	const [sourceView, setSourceView] = useState<"table" | "trace">("table");
	const [combinedView, setCombinedView] = useState<"semantic" | "layout" | "raw">("semantic");
	const project = useQuery({ queryKey: ["project", projectId], queryFn: () => api.getProject(projectId) });
	const sourceQA = useQuery({
		queryKey: ["source-unit-qa", projectId],
		queryFn: () => api.sourceUnitQA(projectId),
		retry: false,
	});
  const resources = useQuery({ queryKey: ["resources", projectId], queryFn: () => api.listResources(projectId) });
	const sourceManifest = useQuery({ queryKey: ["source-manifest", projectId], queryFn: () => api.sourceManifest(projectId) });
	const sourceSegmentation = useQuery({ queryKey: ["source-segmentation", projectId], queryFn: () => api.sourceSegmentation(projectId), retry: false });
  const combinedDocument = useQuery({
    queryKey: ["combined-document", projectId],
    queryFn: () => api.combinedDocument(projectId),
    retry: false,
  });
  const sources = useQuery({ queryKey: ["source-units", projectId, filter, search], queryFn: () => api.sourceUnits(projectId, filter, search) });
  const examples = useQuery({ queryKey: ["examples", projectId], queryFn: () => api.examples(projectId) });
	const needsAttention = sourceQA.data?.qa.needs_attention.length ?? 0;

	useEffect(() => {
		if (needsAttention > 0 && !reviewFilterSelected) {
			setFilter("needs_attention");
			setReviewFilterSelected(true);
		}
	}, [needsAttention, reviewFilterSelected]);

	const resourceItems = resources.data?.items ?? [];
	const readyResources = resourceItems.filter((item) => item.extraction_status === "ready" || item.extraction_status === "needs_attention").length;
	const sourceLines = resourceItems.reduce((sum, item) => sum + (item.line_count ?? 0), 0);
	const documentSummary = combinedDocument.data?.combined_document.summary;
	const unitCount = project.data?.project.counts.source_units ?? 0;
	const plural = (count: number, word: string) => `${count} ${word}${count === 1 ? "" : "s"}`;
	const stripSteps: StripStep[] = [
		{
			title: "Sources", actor: "code",
			state: resourceItems.some((item) => item.extraction_status === "failed") ? "failed" : readyResources > 0 ? "done" : "pending",
			value: plural(readyResources, "document"), detail: sourceLines ? `${sourceLines} lines extracted` : "text extraction", target: "src-resources",
		},
		{
			title: "Segmentation", actor: "LLM",
			state: !documentSummary ? "pending" : documentSummary.fallback_used ? "warn" : "done",
			value: documentSummary ? plural(documentSummary.sentence_count, "sentence") : "—",
			detail: !documentSummary ? "groups lines into sentences" : documentSummary.fallback_used ? "rule-based fallback used" : "grouped by the LLM",
			target: "src-document",
		},
		{
			title: "Segment IDs", actor: "code",
			state: unitCount > 0 ? "done" : "pending",
			value: unitCount > 0 ? plural(unitCount, "unit") : "—", detail: "one unit per segment · list items split", target: unitCount > 0 ? "src-units" : undefined,
		},
		{
			title: "Your review", actor: "you",
			state: unitCount === 0 ? "pending" : needsAttention > 0 ? "warn" : "done",
			value: unitCount === 0 ? "—" : needsAttention > 0 ? `${needsAttention} waiting` : "nothing flagged",
			detail: needsAttention > 0 ? "unclear units need a decision" : "flagged units only", target: sourceQA.data?.qa ? "src-review" : undefined,
		},
	];

	const refreshAfterReview = () => {
		setSelected(null);
		void queryClient.invalidateQueries({ queryKey: ["project", projectId] });
		void queryClient.invalidateQueries({ queryKey: ["project-stages", projectId] });
		void queryClient.invalidateQueries({ queryKey: ["analysis-summary", projectId] });
		void queryClient.invalidateQueries({ queryKey: ["source-units", projectId] });
		void queryClient.invalidateQueries({ queryKey: ["source-unit-qa", projectId] });
	};

  return (
    <>
		<SourceStatusStrip steps={stripSteps} />
      <div className="sources-grid">
        <Panel id="src-resources" title="Input Resources">
          {resources.isLoading ? <LoadingState /> : <ResourceSummaryTable items={resources.data?.items ?? []} />}
        </Panel>
		<Panel id="src-document" title="Validated Combined Document">
			<div className="segmented">
				<button className={combinedView === "semantic" ? "active" : ""} onClick={() => setCombinedView("semantic")}>Sentences</button>
				<button className={combinedView === "layout" ? "active" : ""} onClick={() => setCombinedView("layout")}>Segmentation audit</button>
				<button className={combinedView === "raw" ? "active" : ""} onClick={() => setCombinedView("raw")}>Raw text</button>
			</div>
          {combinedDocument.isLoading ? (
            <LoadingState />
          ) : combinedDocument.data?.combined_document ? (
            <div className="artifact-preview">
              <div className="toolbar">
				<Badge tone={combinedDocument.data.combined_document.summary.fallback_used ? "warn" : "good"}>
					{combinedDocument.data.combined_document.summary.fallback_used ? "deterministic fallback" : "LLM-assisted"}
				</Badge>
                <Badge tone={combinedDocument.data.combined_document.summary.warning_count ? "warn" : "good"}>
                  {combinedDocument.data.combined_document.summary.warning_count} warnings
                </Badge>
                <Badge>{combinedDocument.data.combined_document.summary.resource_count} resources</Badge>
				<Badge tone={combinedDocument.data.combined_document.summary.needs_attention_count ? "warn" : "good"}>
					{combinedDocument.data.combined_document.summary.needs_attention_count} segmentation reviews
				</Badge>
              </div>
				{combinedView === "raw" ? (
					<pre>{combinedDocument.data.combined_document.markdown}</pre>
				) : combinedView === "layout" ? (
					sourceSegmentation.data?.proposal ? <SegmentationList proposal={sourceSegmentation.data.proposal} /> : <p className="muted">Segmentation output is unavailable for this legacy project. Reprocess sources to generate it.</p>
				) : (
					<CombinedSentenceList items={combinedDocument.data.combined_document.lineage.sentences} />
				)}
            </div>
          ) : (
            <p className="muted">No combined document generated.</p>
          )}
        </Panel>
      </div>
		{sourceQA.data?.qa && (
			<Panel
				id="src-review"
				title="Source-unit QA"
				action={needsAttention > 0 ? <Button onClick={() => setFilter("needs_attention")}>Review flagged units</Button> : undefined}
			>
				<div className="toolbar">
					<Badge tone={sourceQA.data.qa.ok ? "good" : "bad"}>{sourceQA.data.qa.ok ? "QA passed" : "QA failed"}</Badge>
					<Badge>{sourceQA.data.qa.derivation_strategy === "llm_classification_backend_normalization" ? "LLM classification + backend normalization" : stageLabel(sourceQA.data.qa.derivation_strategy)}</Badge>
					<Badge tone={sourceQA.data.qa.needs_attention.length ? "warn" : "good"}>
						{sourceQA.data.qa.needs_attention.length} need attention
					</Badge>
					<Badge>
						{sourceQA.data.qa.segments_referenced ?? sourceQA.data.qa.od_sentences_referenced ?? 0}/{sourceQA.data.qa.segments_total ?? sourceQA.data.qa.od_sentences_total ?? 0} evidence segments covered
					</Badge>
				</div>
				{needsAttention > 0 ? (
					<p className="muted">Open a flagged unit, inspect the backend normalization audit, then accept the classification or exclude the unit from modeling.</p>
				) : (
					<p className="muted">All source-unit review gates are resolved. The conceptual model is the next pipeline step.</p>
				)}
			</Panel>
		)}
        <Panel
          id="src-units"
          title="Source Units"
          action={
            <div className="toolbar">
				<Button className={sourceView === "table" ? "active" : ""} onClick={() => setSourceView("table")}>Review table</Button>
				<Button className={sourceView === "trace" ? "active" : ""} onClick={() => setSourceView("trace")}>Trace graph</Button>
              <select className="select" value={filter} onChange={(event) => setFilter(event.target.value)}>
                <option value="all">All</option>
                <option value="needs_attention">Needs attention</option>
                <option value="model_relevant">Model relevant</option>
                <option value="examples">Examples</option>
                <option value="non_model">Non-model</option>
              </select>
              <input className="input" placeholder="Search" value={search} onChange={(event) => setSearch(event.target.value)} />
            </div>
          }
        >
			{sources.isLoading ? <LoadingState /> : sourceView === "trace" ? (
				<SourceTraceGraph
					sentences={combinedDocument.data?.combined_document.lineage.sentences ?? []}
					units={sources.data?.items ?? []}
					onSelectUnit={setSelected}
				/>
			) : (
				<SourceUnitsTable items={sources.data?.items ?? []} filter={filter} onSelect={setSelected} />
			)}
        </Panel>
		<div className="grid-2">
        <Panel title="Structured Examples">
          {examples.isLoading ? <LoadingState /> : <ExamplesTable items={examples.data?.items ?? []} />}
        </Panel>
		<Panel title="Review audit">
			<ReviewAuditSummary decisions={sourceQA.data?.qa.review_decisions ?? []} />
		</Panel>
      </div>
      {selected && (
		<SourceUnitReviewDrawer
			key={selected.id}
			projectId={projectId}
			revision={project.data?.project.current_revision ?? 0}
			unit={selected}
			onClose={() => setSelected(null)}
			onReviewed={refreshAfterReview}
		/>
      )}
    </>
  );
}

function ResourceSummaryTable({ items }: { items: InputResource[] }) {
  if (items.length === 0) return <p className="muted">No input resources.</p>;
  return (
    <table className="data-table">
      <thead>
        <tr>
          <th>Resource</th>
          <th>Status</th>
          <th>Lines</th>
          <th>Hash</th>
        </tr>
      </thead>
      <tbody>
        {items.map((item) => (
          <tr key={item.id}>
            <td>
              <strong>{item.title}</strong>
              <div className="muted truncate">
                {item.id} · {item.file_name || item.kind}
              </div>
            </td>
            <td>
              <StatusBadge value={item.extraction_status} />
            </td>
            <td>{item.line_count ?? 0}</td>
            <td className="truncate">{item.content_hash ? item.content_hash.replace(/^sha256:/, "").slice(0, 10) : "-"}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function SourceUnitsTable({ items, filter, onSelect }: { items: SourceUnit[]; filter: string; onSelect: (unit: SourceUnit) => void }) {
	if (!items.length) return <p className="muted">No source units match this filter.</p>;
  return (
    <table className="data-table">
		<caption className="muted" style={{ textAlign: "left", paddingBottom: 8 }}>Showing {items.length} {filter === "all" ? "source units" : filter.replace(/_/g, " ") + " source units"}</caption>
      <thead>
        <tr>
          <th style={{ width: 130 }}>Unit</th>
			<th>Backend-normalized source text</th>
			<th style={{ width: 120 }}>Kind</th>
			<th style={{ width: 120 }}>Relevance</th>
			<th style={{ width: 105 }}>Normalization</th>
          <th style={{ width: 160 }}>Review</th>
			<th style={{ width: 90 }}>Action</th>
        </tr>
      </thead>
      <tbody>
		{items.map((unit) => (
          <tr key={unit.id}>
            <td>
              <strong>{unit.id}</strong>
              <div className="muted">{unit.section}</div>
            </td>
            <td>{unit.normalized_text}</td>
            <td>{unit.kind}</td>
			<td>{unit.relevance}</td>
			<td><Badge tone={unit.normalization?.changed ? "warn" : "good"}>{unit.normalization?.changed ? "Changed" : "Unchanged"}</Badge></td>
            <td>
              <StatusBadge value={unit.review_status} />
            </td>
			<td><Button onClick={() => onSelect(unit)}>{unit.review_status === "needs_attention" ? "Review" : "View"}</Button></td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function SourceUnitReviewDrawer({
	projectId,
	revision,
	unit,
	onClose,
	onReviewed,
}: {
	projectId: string;
	revision: number;
	unit: SourceUnit;
	onClose: () => void;
	onReviewed: () => void;
}) {
	const [note, setNote] = useState("");
	const review = useMutation({
		mutationFn: (decision: "accept" | "exclude") => api.reviewSourceUnit(projectId, unit.id, {
			base_revision: revision,
			decision,
			note,
		}),
		onSuccess: onReviewed,
	});
	const normalization = unit.normalization;

	return (
		<Drawer title={`Source unit ${unit.id}`} onClose={onClose}>
			<div className="toolbar">
				<StatusBadge value={unit.review_status} />
				<Badge>{unit.confidence} confidence</Badge>
				<Badge>{unit.relevance}</Badge>
			</div>
			{(unit.warnings?.length ?? 0) > 0 && (
				<div className="review-warning-list">
					<strong>{unit.review_status === "needs_attention" ? "Why this needs attention" : "Source notes"}</strong>
					<ul>{unit.warnings?.map((warning) => <li key={warning}>{warning}</li>)}</ul>
				</div>
			)}
			{(unit.requirement_notes?.length ?? 0) > 0 && (
				<div className="requirement-notes">
					<strong>Open modeling questions</strong>
					<ul>{unit.requirement_notes?.map((note) => <li key={note}>{note}</li>)}</ul>
					<p className="muted small">No action needed here: these become design questions in the Review Queue.</p>
				</div>
			)}
			<div className="normalization-comparison">
				<div>
					<div className="label">Exact source text</div>
					<pre>{unit.exact_text ?? ""}</pre>
				</div>
				<div>
					<div className="label">Backend-normalized text</div>
					<pre>{unit.normalized_text}</pre>
				</div>
			</div>
			<Panel
				title="Deterministic normalization audit"
				action={<Badge tone={normalization?.changed ? "warn" : "good"}>{normalization?.changed ? "Changed" : "Unchanged"}</Badge>}
			>
				{normalization ? (
					<div className="normalization-audit">
						<div className="toolbar">
							<Badge>{normalization.version}</Badge>
							<Badge tone="good">{normalization.strategy}</Badge>
						</div>
						<div className="hash-grid">
							<div><span className="muted">Exact hash</span><code>{shortAuditHash(normalization.exact_hash)}</code></div>
							<div><span className="muted">Normalized hash</span><code>{shortAuditHash(normalization.normalized_hash)}</code></div>
						</div>
						{normalization.operations.length ? normalization.operations.map((operation, index) => (
							<div className="normalization-operation" key={`${operation.kind}-${index}`}>
								<strong>{index + 1}. {humanizeOperation(operation.kind)}</strong>
								<div><span>Before</span><pre>{operation.before}</pre></div>
								<div><span>After</span><pre>{operation.after}</pre></div>
							</div>
						)) : <p className="muted">No normalization changes were necessary.</p>}
					</div>
				) : <p className="muted">Normalization audit is unavailable for this legacy unit.</p>}
			</Panel>
			<p className="muted">Origins: {unit.origin_spans.map((span) => span.label).join(", ") || "No origin span recorded"}</p>
			<Field label="Review note (optional)">
				<textarea className="textarea" value={note} onChange={(event) => setNote(event.target.value)} disabled={unit.review_status !== "needs_attention" || review.isPending} />
			</Field>
			{review.isError && <p className="error-text">{review.error instanceof Error ? review.error.message : "Source unit could not be reviewed."}</p>}
			{unit.review_status === "needs_attention" ? (
				<div className="review-actions">
					<Button variant="primary" disabled={review.isPending} onClick={() => review.mutate("accept")}>Accept unit</Button>
					<Button variant="danger" disabled={review.isPending} onClick={() => review.mutate("exclude")}>Mark as non-model</Button>
				</div>
			) : (
				<p className="muted">This unit has already passed source review.</p>
			)}
			<div className="toolbar">
				{unit.linked_requirements.map((id) => <Badge key={id}>{id}</Badge>)}
				{unit.open_review_candidates.map((id) => <Badge tone="warn" key={id}>{id}</Badge>)}
			</div>
		</Drawer>
	);
}

function shortAuditHash(value: string) {
	if (!value) return "unavailable";
	const normalized = value.replace(/^sha256:/, "");
	return `sha256:${normalized.slice(0, 12)}…`;
}

function humanizeOperation(value: string) {
	return value.replace(/_/g, " ").replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function ExamplesTable({ items }: { items: StructuredExample[] }) {
  if (!items.length) return <p className="muted">No structured examples detected.</p>;
  return (
    <table className="data-table">
      <thead>
        <tr>
          <th>Example</th>
          <th>Authority</th>
          <th>Fields</th>
        </tr>
      </thead>
      <tbody>
        {items.map((item) => (
          <tr key={item.id}>
            <td>
              <strong>{item.title}</strong>
              <div className="muted">{item.type}</div>
            </td>
            <td>
              <StatusBadge value={item.authority} />
            </td>
            <td>{item.parsed_fields.map((field) => field.path).join(", ")}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function RequirementsTab({ projectId }: { projectId: string }) {
  const [filter, setFilter] = useState("all");
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<RequirementAtom | null>(null);
  const requirements = useQuery({
    queryKey: ["requirements", projectId, filter, search],
    queryFn: () => api.requirements(projectId, filter, search),
  });
	const obligations = useQuery({ queryKey: ["design-obligations", projectId], queryFn: () => api.designObligations(projectId), retry: false });
  const coverage = requirements.data?.coverage;
  return (
    <>
      <div className="grid-3">
        <Metric label="Source units covered" value={coverage?.source_units_covered ?? "-"} />
        <Metric label="Direct DB requirements" value={coverage?.direct_db_requirements ?? "-"} />
        <Metric label="Need review" value={coverage?.requirements_needing_review ?? "-"} />
      </div>
      <Panel
        title="Requirement Atoms"
        action={
          <div className="toolbar">
            <select className="select" value={filter} onChange={(event) => setFilter(event.target.value)}>
              <option value="all">All</option>
              <option value="needs_review">Needs review</option>
              <option value="direct_db">Direct DB</option>
              <option value="non_model">Non-model</option>
            </select>
            <input className="input" placeholder="Search" value={search} onChange={(event) => setSearch(event.target.value)} />
          </div>
        }
      >
        <table className="data-table">
          <thead>
            <tr>
              <th style={{ width: 90 }}>Atom</th>
              <th>Statement</th>
              <th style={{ width: 170 }}>Type</th>
              <th style={{ width: 120 }}>Outcome</th>
              <th style={{ width: 110 }}>Review</th>
            </tr>
          </thead>
          <tbody>
            {(requirements.data?.items ?? []).map((item) => (
              <tr key={item.id} className="clickable-row" onClick={() => setSelected(item)}>
                <td>
                  <strong>{item.id}</strong>
                </td>
                <td>{item.statement}</td>
                <td className="muted">{humanizeStatus(item.atom_type)}</td>
                <td><StatusBadge value={item.modeling_outcome} /></td>
                <td>
                  <StatusBadge value={item.review_status} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </Panel>
		{obligations.data && (
			<Panel title={`Design Obligations · ${obligations.data.design_obligations.length}`}>
				<table className="data-table"><thead><tr><th>Obligation</th><th style={{ width: 150 }}>Kind</th><th style={{ width: 130 }}>Persistence</th><th style={{ width: 90 }}>Risk</th></tr></thead><tbody>
					{obligations.data.design_obligations.map((item) => <tr key={item.id}><td><strong>{item.id}</strong><div>{item.statement}</div></td><td><Badge>{humanizeStatus(item.kind)}</Badge></td><td className="muted">{humanizeStatus(item.persistence)}</td><td><Badge tone={item.risk === "high" ? "warn" : item.risk === "low" ? "good" : "default"}>{item.risk}</Badge></td></tr>)}
				</tbody></table>
			</Panel>
		)}
      {selected && (
        <Drawer title={selected.id} onClose={() => setSelected(null)}>
          <p>{selected.statement}</p>
          <div className="toolbar">
            <Badge>{selected.atom_type}</Badge>
            <Badge>{selected.modeling_relevance}</Badge>
            <StatusBadge value={selected.review_status} />
			<Badge tone={(selected.review_class ?? "none").startsWith("blocking_") ? "warn" : "default"}>{humanizeStatus(selected.review_class ?? "none")}</Badge>
          </div>
		  {(selected.review_class ?? "none") !== "none" && (
			<Panel title={`Review signal · ${humanizeStatus(selected.review_topic ?? "other")}`}>
			  {(selected.warnings ?? []).length > 0 ? selected.warnings.map((warning) => <p key={warning}>{warning}</p>) : <p className="muted">No additional warning was recorded.</p>}
			</Panel>
		  )}
          <Panel title="Source evidence">
            <div className="toolbar">
              {selected.source_units.map((id) => (
                <Badge key={id}>{id}</Badge>
              ))}
            </div>
          </Panel>
          <Panel title="Model impact">
            <div className="toolbar">
              {selected.model_impact_preview.map((id) => (
                <Badge key={id}>{id}</Badge>
              ))}
            </div>
          </Panel>
		  <Panel title="Atomic semantics">
			<p><strong>{selected.subject || "—"}</strong> · {selected.predicate || "—"} · {selected.object || "—"}</p>
			<p className="muted">Quantifier: {selected.quantifier || "not stated"} · Condition: {selected.condition || "not stated"} · Time: {selected.temporal_semantics || "not stated"} · Owner: {selected.ownership || "not stated"}</p>
		  </Panel>
        </Drawer>
      )}
    </>
  );
}

function FunctionalCrudTab({ projectId }: { projectId: string }) {
  const [area, setArea] = useState<string>("all");
  const areas = useQuery({ queryKey: ["functional-areas", projectId], queryFn: () => api.functionalAreas(projectId) });
  const actors = useQuery({ queryKey: ["actors", projectId], queryFn: () => api.actors(projectId) });
  const operations = useQuery({ queryKey: ["crud-operations", projectId], queryFn: () => api.crudOperations(projectId) });
	const requirements = useQuery({ queryKey: ["requirements", projectId, "all", ""], queryFn: () => api.requirements(projectId) });
	const areaItems = areas.data?.items ?? [];
	const selectedArea = areaItems.find((item) => item.id === area);
	const selectedAtomIDs = new Set(selectedArea?.requirement_atoms ?? []);
	const mappedRequirements = (requirements.data?.items ?? []).filter((requirement) => area === "all" || selectedAtomIDs.has(requirement.id));
	const areaByAtom = new Map<string, FunctionalArea>();
	for (const functionalArea of areaItems) {
		for (const atomID of functionalArea.requirement_atoms) areaByAtom.set(atomID, functionalArea);
	}
	const actorByID = new Map((actors.data?.items ?? []).map((actor) => [actor.id, actor]));
  const filteredOperations = (operations.data?.items ?? []).filter((operation) => area === "all" || operation.functional_area_id === area);
  return (
    <div className="grid-2">
      <Panel title="Functional Areas">
        <div className="field" style={{ gap: 8 }}>
          <button className={`nav-item ${area === "all" ? "active" : ""}`} onClick={() => setArea("all")}>
			<span>All areas</span>
			<Badge>{areaItems.length}</Badge>
          </button>
			{areas.isLoading && <LoadingState />}
          {areaItems.map((item: FunctionalArea) => (
            <button className={`nav-item ${area === item.id ? "active" : ""}`} key={item.id} onClick={() => setArea(item.id)}>
              <span>{item.label}</span>
			  <Badge tone={item.open_review_candidates.length > 0 ? "warn" : "default"}>{item.requirement_atoms.length} items</Badge>
            </button>
          ))}
        </div>
      </Panel>
		<Panel title={selectedArea?.label ?? "Functional Analysis"}>
			{selectedArea ? (
				<div className="field" style={{ gap: 14 }}>
					<p>{selectedArea.purpose}</p>
					<div>
						<strong>Modeling focus</strong>
						<ul>{selectedArea.modeling_focus.map((focus) => <li key={focus}>{focus}</li>)}</ul>
					</div>
					<div>
						<strong>Main actors</strong>
						<div className="toolbar" style={{ marginTop: 8 }}>
							{selectedArea.main_actors.map((actorID) => <Badge key={actorID}>{actorByID.get(actorID)?.label ?? actorID}</Badge>)}
						</div>
					</div>
					<p className="muted">{selectedArea.requirement_atoms.length} requirement atoms are mapped to this area.</p>
				</div>
			) : (
				<div>
					<p>Functional analysis produced {areaItems.length} areas and mapped {requirements.data?.items.length ?? 0} requirement atoms.</p>
					<p className="muted">Select an area to inspect its purpose, modeling focus, actors, and contained requirements.</p>
				</div>
			)}
      </Panel>
		<div className="grid-span-full">
			<Panel title={`Mapped Requirement Atoms · ${mappedRequirements.length}`}>
				{requirements.isLoading ? <LoadingState /> : <FunctionalRequirementsTable items={mappedRequirements} areaByAtom={areaByAtom} showArea={area === "all"} />}
			</Panel>
		</div>
		<div className="grid-span-full">
		<Panel title="Actors">
			{actors.isLoading ? <LoadingState /> : (
				<div className="toolbar">
					{(actors.data?.items ?? []).map((actor) => <Badge key={actor.id}>{actor.label} · {actor.kind}</Badge>)}
				</div>
			)}
		</Panel>
		</div>
		<div className="grid-span-full">
		<Panel title={`CRUD Operations · ${filteredOperations.length}`}>
			{operations.isLoading ? <LoadingState /> : filteredOperations.length > 0 ? (
				<CrudTable items={filteredOperations} actorLabel={(id) => actorByID.get(id)?.label ?? id} />
			) : (
				<p className="muted">CRUD operations are generated by the next pipeline stage, Build CRUD Mapping. The functional-analysis items are shown above.</p>
			)}
		</Panel>
		</div>
    </div>
  );
}

function FunctionalRequirementsTable({
	items,
	areaByAtom,
	showArea,
}: {
	items: RequirementAtom[];
	areaByAtom: Map<string, FunctionalArea>;
	showArea: boolean;
}) {
	if (!items.length) return <p className="muted">No requirement atoms are mapped to this area.</p>;
	return (
		<table className="data-table">
			<thead>
				<tr>
					<th style={{ width: 90 }}>Atom</th>
					<th>Requirement</th>
					{showArea && <th style={{ width: "20%" }}>Area</th>}
					<th style={{ width: 150 }}>Type</th>
					<th style={{ width: 120 }}>Relevance</th>
					<th style={{ width: 110 }}>Sources</th>
				</tr>
			</thead>
			<tbody>
				{items.map((item) => (
					<tr key={item.id}>
						<td><strong>{item.id}</strong></td>
						<td>{item.statement}</td>
						{showArea && <td>{areaByAtom.get(item.id)?.label ?? "Unmapped"}</td>}
						<td className="muted">{humanizeStatus(item.atom_type)}</td>
						<td><StatusBadge value={item.modeling_relevance} /></td>
						<td className="muted small">{item.source_units.join(", ")}</td>
					</tr>
				))}
			</tbody>
		</table>
	);
}

function CrudTable({ items, actorLabel }: { items: CrudOperation[]; actorLabel: (id: string) => string }) {
  const effects = (label: string, tone: string, values: string[]) =>
    values.map((value) => <span key={`${label}:${value}`} className={`crud-chip ${tone}`} title={label}>{label} {value}</span>);
  return (
    <table className="data-table">
      <thead>
        <tr>
          <th style={{ width: "30%" }}>Operation</th>
          <th style={{ width: 150 }}>Actor</th>
          <th>Data effects</th>
        </tr>
      </thead>
      <tbody>
        {items.map((item) => (
          <tr key={item.id}>
            <td>
              <strong>{item.label}</strong>
              <div className="muted small">{item.outcome}</div>
            </td>
            <td>{actorLabel(item.actor_id)}</td>
            <td>
              <div className="crud-effects">
                {effects("C", "create", item.creates)}
                {effects("R", "read", item.reads)}
                {effects("U", "update", item.updates)}
                {effects("D", "delete", item.deletes)}
                {item.creates.length + item.reads.length + item.updates.length + item.deletes.length === 0 && <span className="muted small">no persistent effect</span>}
              </div>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function ReviewTab({
	projectId,
	revision,
	nextAction,
	onNext,
	onDecisionsApplied,
	nextDisabled,
}: {
	projectId: string;
	revision: number;
	nextAction: string;
	onNext: () => void;
	onDecisionsApplied: () => void;
	nextDisabled: boolean;
}) {
  const queryClient = useQueryClient();
  const [job, setJob] = useState<Job | null>(null);
	const [selected, setSelected] = useState<Record<string, string>>({});
	const [reviewStartedAt] = useState(() => Date.now());
  const candidates = useQuery({ queryKey: ["review-candidates", projectId], queryFn: () => api.reviewCandidates(projectId) });
	const answer = useMutation({
		mutationFn: () => api.answerReviewBatch(
			projectId,
			revision,
			Object.entries(selected).map(([candidate_id, selected_option_id]) => ({ candidate_id, selected_option_id })),
			Date.now() - reviewStartedAt,
		),
    onSuccess: ({ job }) => setJob(job),
  });
	const all = candidates.data?.items ?? [];
	const open = all.filter((item) => item.status === "open");
	const selectionCount = Object.keys(selected).length;

  if (job) {
    return (
      <Panel title="Refreshing analysis">
        <JobProgress
          projectId={projectId}
          job={job}
          onDone={() => {
            setJob(null);
            void queryClient.invalidateQueries().then(onDecisionsApplied);
          }}
		  onDismiss={() => setJob(null)}
        />
      </Panel>
    );
  }

  if (!open.length) {
    return (
      <Panel title="No more to review">
        <div className="toolbar">
          <CheckCircle2 size={20} color="#0f766e" />
          <span>All blocking review questions are resolved.</span>
			<Button variant="primary" onClick={onNext} disabled={nextDisabled}>
				<Play size={18} />
				{nextAction}
          </Button>
        </div>
      </Panel>
    );
  }

  return (
    <div className="page">
		<Panel title={`Review Queue · ${open.length} open`}>
		<div className="toolbar" style={{ marginBottom: 16 }}>
			<span>{selectionCount} selected for one deterministic batch.</span>
			<Button variant="primary" onClick={() => answer.mutate()} disabled={answer.isPending || selectionCount === 0}>
				Apply selected decisions
			</Button>
			{answer.error && <span className="error-text">{answer.error.message}</span>}
		</div>
        <div className="field" style={{ gap: 14 }}>
			{open.map((candidate: ReviewCandidate) => {
				const unresolvedDependencies = unresolvedReviewDependencies(candidate, all).filter((id) => !selected[id]);
				const locked = unresolvedDependencies.length > 0;
				return (
            <div className="panel" key={candidate.id}>
              <div className="panel-body">
                <div className="toolbar">
                  <GitBranch size={18} />
                  <strong>{candidate.id}</strong>
					<Badge tone={candidate.blocking ? "bad" : "warn"}>{candidate.blocking ? "blocking" : "non-blocking"}</Badge>
					{candidate.severity && <StatusBadge value={candidate.severity} />}
					{candidate.recommendation_confidence && <Badge>{candidate.recommendation_confidence} recommendation confidence</Badge>}
                </div>
                <h3 className="panel-title" style={{ marginTop: 12 }}>
                  {candidate.question}
                </h3>
                <p className="muted">{candidate.description}</p>
				<p><strong>Why this matters:</strong> {candidate.description}</p>
				<p className="muted">Impact: {candidate.may_affect.join(", ") || "No downstream impact declared."}</p>
				<div className="toolbar">
					{(candidate.affected_source_units ?? []).map((id) => <Badge key={id}>{id}</Badge>)}
					{candidate.affected_atoms.map((id) => <Badge key={id}>{id}</Badge>)}
				</div>
				{locked && <p className="error-text">Resolve {unresolvedDependencies.join(", ")} before answering this question.</p>}
				<div className="review-option-grid">
                  {candidate.options.map((option) => (
					<div className={`review-option-card ${option.recommended ? "recommended" : ""}`} key={option.id} aria-selected={selected[candidate.id] === option.id}>
						<div className="toolbar"><strong>{option.label}</strong>{option.recommended && <Badge tone="good">Recommended</Badge>}</div>
						<p>{option.rationale}</p>
						{option.effect_summary && <p className="muted"><strong>Effect:</strong> {option.effect_summary}</p>}
						{(option.benefits?.length ?? 0) > 0 && <p className="muted"><strong>Benefits:</strong> {option.benefits?.join("; ")}</p>}
						{(option.risks?.length ?? 0) > 0 && <p className="muted"><strong>Risks:</strong> {option.risks?.join("; ")}</p>}
						{option.effects?.impact_dimensions?.length ? <p className="muted"><strong>Risk dimensions:</strong> {option.effects.impact_dimensions.join(", ")}</p> : null}
						<Button variant={selected[candidate.id] === option.id || option.recommended ? "primary" : "default"} onClick={() => setSelected((current) => ({ ...current, [candidate.id]: option.id }))} disabled={answer.isPending || locked}>
							{selected[candidate.id] === option.id ? "Selected" : "Choose option"}
						</Button>
					</div>
                  ))}
                </div>
              </div>
            </div>
			);
			})}
        </div>
      </Panel>
    </div>
  );
}
