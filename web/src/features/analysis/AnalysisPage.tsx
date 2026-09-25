import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertCircle, CheckCircle2, Circle, Loader2, Play, XCircle } from "lucide-react";
import { api } from "@/shared/api/client";
import type {
	CombinedDocumentSentence,
  InputResource,
  Job,
  SourceUnit,
	ProjectStageName,
	SourceSegmentationProposal,
} from "@/shared/api/types";
import { Badge, Button, Drawer, Field, LoadingState, Metric, Panel, StatusBadge } from "@/shared/components/ui";
import { humanizeStatus } from "@/shared/lib/status";
import { useRouter } from "@/shared/lib/router";
import { JobProgress, friendlyError } from "@/features/jobs/JobProgress";
import { PipelineStepper } from "./PipelineStepper";
import { SourceTraceGraph } from "./SourceTraceGraph";
import { isRunnableStage, isTerminalJobStatus, nextStageLabel, shouldRecoverLatestJob } from "@/shared/lib/pipeline";
import { consumeAutoRun, gatePath } from "@/shared/lib/autopilot";

const tabs = [
	["overview", "Overview"],
  ["sources", "Sources"],
	["activity", "Activity & LLM Runs"],
] as const;

export function AnalysisPage({ projectId, mode }: { projectId: string; mode: string }) {
  const { navigate } = useRouter();
	const queryClient = useQueryClient();
  const project = useQuery({ queryKey: ["project", projectId], queryFn: () => api.getProject(projectId) });
	const stages = useQuery({ queryKey: ["project-stages", projectId], queryFn: () => api.stageStatus(projectId) });
	const jobs = useQuery({ queryKey: ["jobs", projectId], queryFn: () => api.jobs(projectId), retry: false, refetchInterval: 2000 });
	const [job, setJob] = useState<Job | null>(null);
	const [dismissedJobIds, setDismissedJobIds] = useState<Set<string>>(() => new Set());
	const completedJobIds = useRef<Set<string>>(new Set());
	const [autoRun, setAutoRun] = useState(() => consumeAutoRun(projectId));
	const runNext = useMutation({
		mutationFn: ({ stage, revision }: { stage: ProjectStageName; revision: number }) => {
			// Source processing has its own route that also carries the LLM options.
			return stage === "process_sources"
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

	const health = stages.data?.artifact_health ?? project.data?.artifact_health;
	const nextStage = stages.data?.next_stage;
	const nextAction = nextStageLabel(nextStage);
	const handleNext = () => {
		if (isRunnableStage(nextStage)) {
			setAutoRun(true);
			runNext.mutate({ stage: nextStage, revision: project.data?.project.current_revision ?? 0 });
			return;
		}
		const path = gatePath(projectId, nextStage, project.data?.project.lifecycle_status);
		if (path) navigate(path);
	};
	const onStageDone = async (completedJob: Job) => {
		if (completedJobIds.current.has(completedJob.id)) return;
		completedJobIds.current.add(completedJob.id);
		setJob((current) => current?.id === completedJob.id ? null : current);
		await queryClient.invalidateQueries();
		if (autoRun) await advance(completedJob.stage);
		else if (completedJob.stage === "process_sources" && mode === "overview") navigate(`/projects/${projectId}/analysis/sources`);
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
				<Play size={18} />
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
				onDone={(completedJob) => void onStageDone(completedJob)}
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
        {tabs.map(([id, label]) => (
          <button
            className={`tab ${mode === id ? "active" : ""}`}
            key={id}
            onClick={() => navigate(`/projects/${projectId}/analysis/${id}`)}
          >
            {label}
          </button>
        ))}
      </div>

		{project.isLoading || stages.isLoading ? (
        <LoadingState />
		) : mode === "overview" ? (
			<ProjectOverview projectId={projectId} nextAction={nextAction} onNext={handleNext} disabled={!nextStage || !!job || runNext.isPending} />
		) : mode === "activity" ? (
			<ActivityView projectId={projectId} />
      ) : (
        <SourcesTab projectId={projectId} />
      )}
    </div>
  );
}

function ActivityView({ projectId }: { projectId: string }) {
	const jobs = useQuery({
		queryKey: ["jobs", projectId], queryFn: () => api.jobs(projectId), retry: false,
		refetchInterval: (query) => query.state.data?.items.some((job) => !isTerminalJobStatus(job.status)) ? 2000 : false,
	});
	const hasActiveJob = jobs.data?.items.some((job) => !isTerminalJobStatus(job.status)) ?? false;
	const runs = useQuery({
		queryKey: ["llm-runs", projectId], queryFn: () => api.llmRuns(projectId), retry: false,
		refetchInterval: hasActiveJob ? 2000 : false,
	});
	const optimization = useQuery({
		queryKey: ["optimization-report", projectId], queryFn: () => api.optimizationReport(projectId), retry: false,
		refetchInterval: hasActiveJob ? 2000 : false,
	});
	const metrics = optimization.data?.report;
	return (
		<div className="activity-stack">
			<Panel title="LLM optimization report">
				{optimization.isLoading ? <LoadingState /> : optimization.isError ? (
					<p className="error-text">{(optimization.error as Error).message}</p>
				) : <>
					<div className="grid-3">
						<Metric label="Provider calls" value={metrics?.totals.provider_calls ?? 0} />
						<Metric label="Cache hits" value={metrics?.totals.cache_hits ?? 0} />
						<Metric label="Retries" value={metrics?.totals.retries ?? 0} />
						<Metric label="Tokens" value={(metrics?.totals.total_tokens ?? 0).toLocaleString("en-US")} />
						<Metric label="Context saved" value={`${((metrics?.totals.context_reduction_ratio ?? 0) * 100).toFixed(1)}%`} />
						<Metric label="Usage unknown" value={metrics?.totals.unknown_usage_attempts ?? 0} />
					</div>
					<p className="muted">Policies: {metrics?.budget_policy ?? "-"} · {metrics?.context_policy ?? "-"} · {metrics?.call_gate_policy ?? "-"}</p>
				</>}
			</Panel>
			<Panel title={`Jobs · ${jobs.data?.items.length ?? 0}`}>
				{jobs.isLoading ? <LoadingState /> : jobs.isError ? <p className="error-text">{(jobs.error as Error).message}</p> : <>
				<table className="data-table"><thead><tr><th>Stage</th><th style={{ width: 110 }}>Status</th><th style={{ width: 90 }}>Revision</th><th style={{ width: 80 }}>Progress</th></tr></thead><tbody>
					{(jobs.data?.items ?? []).map((job) => <tr key={job.id}><td><strong>{stageLabel(job.stage)}</strong><div className="muted">{job.id}</div></td><td><StatusBadge value={job.status} />{job.error && <div className="error-text small">{friendlyError(job.error)}</div>}</td><td>{job.input_revision || "-"} → {job.output_revision || "-"}</td><td>{job.progress}%</td></tr>)}
				</tbody></table>
				{!jobs.data?.items.length && <p className="muted">No jobs recorded.</p>}
				</>}
			</Panel>
			<Panel title={`Sanitized LLM runs · ${runs.data?.items.length ?? 0}`}>
				{runs.isLoading ? <LoadingState /> : runs.isError ? <p className="error-text">{(runs.error as Error).message}</p> : <>
				<table className="data-table"><thead><tr><th>Stage / model</th><th>Status</th><th>Duration</th><th>Tokens</th></tr></thead><tbody>
					{(runs.data?.items ?? []).map((run) => <tr key={run.id}><td><strong>{stageLabel(run.stage)}</strong><div className="muted">{run.provider} · {run.model}</div><div className="muted">validation: {run.validation_scope ?? "structured schema"}</div></td><td><StatusBadge value={run.status} />{(run.errors?.length ?? 0) > 0 && <div className="error-text">{run.errors!.length} issue(s)</div>}</td><td>{formatDuration(run.duration_ms ?? 0)}</td><td>{run.usage.total_tokens ? run.usage.total_tokens.toLocaleString("en-US") : "-"}</td></tr>)}
				</tbody></table>
				{!runs.data?.items.length && <p className="muted">No LLM runs recorded.</p>}
				</>}
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

function SourcesTab({ projectId }: { projectId: string }) {
	const queryClient = useQueryClient();
  const [filter, setFilter] = useState("all");
	const [reviewFilterSelected, setReviewFilterSelected] = useState(false);
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<SourceUnit | null>(null);
	const [sourceView, setSourceView] = useState<"table" | "trace">("table");
	const [combinedView, setCombinedView] = useState<"semantic" | "layout" | "raw">("semantic");
	const project = useQuery({ queryKey: ["project", projectId], queryFn: () => api.getProject(projectId) });
	const sourceUnitsReady = project.data?.artifact_health.source_units_status !== undefined
		&& project.data.artifact_health.source_units_status !== "not_generated";
	const combinedDocumentReady = project.data?.artifact_health.combined_document_status === "ready";
	const sourceQA = useQuery({
		queryKey: ["source-unit-qa", projectId],
		queryFn: () => api.sourceUnitQA(projectId),
		retry: false,
		enabled: sourceUnitsReady,
	});
  const resources = useQuery({ queryKey: ["resources", projectId], queryFn: () => api.listResources(projectId) });
	const sourceSegmentation = useQuery({ queryKey: ["source-segmentation", projectId], queryFn: () => api.sourceSegmentation(projectId), retry: false, enabled: combinedDocumentReady });
  const combinedDocument = useQuery({
    queryKey: ["combined-document", projectId],
    queryFn: () => api.combinedDocument(projectId),
    retry: false,
		enabled: combinedDocumentReady,
  });
  const sources = useQuery({
		queryKey: ["source-units", projectId, filter, search],
		queryFn: () => api.sourceUnits(projectId, filter, search),
		enabled: sourceUnitsReady,
	});
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
	          {!combinedDocumentReady ? (
			<p className="muted">The combined document will appear when source segmentation completes.</p>
		  ) : combinedDocument.isLoading ? (
            <LoadingState />
		  ) : combinedDocument.isError ? (
			<p className="error-text">{(combinedDocument.error as Error).message}</p>
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
		{sourceQA.isError && <p className="error-text">Source-unit QA could not be loaded: {(sourceQA.error as Error).message}</p>}
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
                <option value="non_model">Non-model</option>
              </select>
              <input className="input" placeholder="Search" value={search} onChange={(event) => setSearch(event.target.value)} />
            </div>
          }
        >
			{!sourceUnitsReady ? (
				<p className="muted">Source units are not ready yet. They will appear automatically when segmentation completes.</p>
			) : sources.isLoading ? <LoadingState /> : sources.isError ? (
				<p className="error-text">Source units could not be loaded: {(sources.error as Error).message}</p>
			) : sourceView === "trace" ? (
				<SourceTraceGraph
					sentences={combinedDocument.data?.combined_document.lineage.sentences ?? []}
					units={sources.data?.items ?? []}
					onSelectUnit={setSelected}
				/>
			) : (
				<SourceUnitsTable items={sources.data?.items ?? []} filter={filter} onSelect={setSelected} />
			)}
        </Panel>
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
			<td>{unit.relevance ? humanizeStatus(unit.relevance) : "—"}</td>
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
				{unit.relevance && <Badge>{humanizeStatus(unit.relevance)}</Badge>}
			</div>
			{(unit.warnings?.length ?? 0) > 0 && (
				<div className="review-warning-list">
					<strong>{unit.review_status === "needs_attention" ? "Why this needs attention" : "Source notes"}</strong>
					<ul>{unit.warnings?.map((warning) => <li key={warning}>{warning}</li>)}</ul>
				</div>
			)}
			{(unit.requirement_notes?.length ?? 0) > 0 && (
				<div className="requirement-notes">
					<strong>Modeling notes from segmentation</strong>
					<ul>{unit.requirement_notes?.map((note) => <li key={note}>{note}</li>)}</ul>
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
