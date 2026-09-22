import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertCircle, CheckCircle2, FileCheck2, GitBranch, Languages, Play, ScanText, Sparkles } from "lucide-react";
import { api } from "@/shared/api/client";
import type {
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
	SourceFidelityReport,
	SourceSegmentationProposal,
	SourceUnitQA,
} from "@/shared/api/types";
import { Badge, Button, Drawer, Field, LoadingState, Metric, Panel, StatusBadge } from "@/shared/components/ui";
import { useRouter } from "@/shared/lib/router";
import { JobProgress } from "@/features/jobs/JobProgress";
import { PipelineStepper } from "./PipelineStepper";
import { SourceTraceGraph } from "./SourceTraceGraph";
	import { isRunnableStage, nextStageLabel, shouldRecoverLatestJob, unresolvedReviewDependencies } from "@/shared/lib/pipeline";

const tabs = [
	["overview", "Overview"],
  ["sources", "Sources & Examples"],
  ["requirements", "Requirements"],
  ["functional-crud", "Functional / CRUD"],
	["review", "Review Queue"],
	["activity", "Activity & LLM Runs"],
] as const;

export function AnalysisPage({ projectId, mode }: { projectId: string; mode: string }) {
  const { navigate } = useRouter();
	const queryClient = useQueryClient();
  const project = useQuery({ queryKey: ["project", projectId], queryFn: () => api.getProject(projectId) });
  const summary = useQuery({ queryKey: ["analysis-summary", projectId], queryFn: () => api.analysisSummary(projectId) });
	const stages = useQuery({ queryKey: ["project-stages", projectId], queryFn: () => api.stageStatus(projectId) });
	const jobs = useQuery({ queryKey: ["jobs", projectId], queryFn: () => api.jobs(projectId), retry: false, refetchInterval: 2000 });
  const [job, setJob] = useState<Job | null>(null);
	const [dismissedJobIds, setDismissedJobIds] = useState<Set<string>>(() => new Set());
	const runNext = useMutation({
		mutationFn: (stage: ProjectStageName) => {
			const revision = project.data?.project.current_revision ?? 0;
			return stage === "combined_document"
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
  });
	const latestJob = jobs.data?.items[0];

	useEffect(() => {
		if (job || !latestJob || dismissedJobIds.has(latestJob.id) || !shouldRecoverLatestJob(latestJob.status)) return;
		setJob(latestJob);
	}, [dismissedJobIds, job, latestJob]);

  const openReviews = project.data?.project.counts.open_review_questions ?? 0;
	const health = stages.data?.artifact_health ?? project.data?.artifact_health;
	const nextStage = stages.data?.next_stage;
	const nextAction = nextStageLabel(nextStage, openReviews);
	const handleNext = () => {
		if (nextStage === "source_review") navigate(`/projects/${projectId}/analysis/sources`);
		else if (nextStage === "review_decisions") navigate(`/projects/${projectId}/analysis/review`);
		else if (nextStage === "conceptual_review") navigate(`/projects/${projectId}/model/conceptual`);
		else if (nextStage === "model_review") navigate(`/projects/${projectId}/model/trace`);
		else if (nextStage === "completed") navigate(`/projects/${projectId}/dbml`);
		else if (isRunnableStage(nextStage)) runNext.mutate(nextStage);
	};

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1 className="page-title">{project.data?.project.name ?? "Project"} | Analysis Workspace</h1>
          <p className="page-subtitle">
            {project.data?.project.counts.source_units ?? 0} source units · {project.data?.project.counts.requirements ?? 0} requirements ·{" "}
            {project.data?.project.counts.operations ?? 0} operations · {openReviews} reviews open
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
				onDone={() => {
					const completedStage = job.stage;
					setJob(null);
					void queryClient.invalidateQueries();
					if (completedStage === "process_sources" || completedStage === "combined_document") {
						navigate(`/projects/${projectId}/analysis/sources`);
					}
				}}
				onDismiss={(dismissed) => {
					setDismissedJobIds((current) => new Set(current).add(dismissed.id));
					setJob(null);
				}}
			/>
        </Panel>
      )}
		{runNext.isError && <p className="error-text">{runNext.error instanceof Error ? runNext.error.message : "Stage could not be started."}</p>}

      <div className="grid-3">
        <Metric label="Source units" value={project.data?.project.counts.source_units ?? "-"} />
        <Metric label="Requirements" value={project.data?.project.counts.requirements ?? "-"} />
        <Metric label="Open reviews" value={openReviews} />
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
		<div className="grid-2">
			<Panel title="LLM optimization report">
				<div className="grid-3">
					<Metric label="Provider calls" value={metrics?.totals.provider_calls ?? 0} />
					<Metric label="Cache hits" value={metrics?.totals.cache_hits ?? 0} />
					<Metric label="Retries" value={metrics?.totals.retries ?? 0} />
					<Metric label="Tokens" value={metrics?.totals.total_tokens ?? 0} />
					<Metric label="Context saved" value={`${((metrics?.totals.context_reduction_ratio ?? 0) * 100).toFixed(1)}%`} />
					<Metric label="Review calls avoided" value={metrics?.avoided_review_resolution_calls ?? 0} />
					<Metric label="Generation calls avoided" value={metrics?.avoided_generation_calls ?? 0} />
					<Metric label="Usage unknown" value={metrics?.totals.unknown_usage_attempts ?? 0} />
				</div>
				<p className="muted">Policies: {metrics?.budget_policy ?? "-"} · {metrics?.context_policy ?? "-"} · {metrics?.call_gate_policy ?? "-"}</p>
			</Panel>
			<Panel title={`Jobs · ${jobs.data?.items.length ?? 0}`}>
				<table className="data-table"><thead><tr><th>Stage</th><th>Status</th><th>Revision</th><th>Progress</th></tr></thead><tbody>
					{(jobs.data?.items ?? []).map((job) => <tr key={job.id}><td><strong>{stageLabel(job.stage)}</strong><div className="muted">{job.id}</div></td><td><StatusBadge value={job.status} />{job.error && <div className="error-text">{job.error}</div>}</td><td>{job.input_revision || "-"} → {job.output_revision || "-"}</td><td>{job.progress}%</td></tr>)}
				</tbody></table>
				{!jobs.data?.items.length && <p className="muted">No jobs recorded.</p>}
			</Panel>
			<Panel title={`Sanitized LLM runs · ${runs.data?.items.length ?? 0}`}>
				<table className="data-table"><thead><tr><th>Stage / model</th><th>Status</th><th>Duration</th><th>Tokens</th></tr></thead><tbody>
					{(runs.data?.items ?? []).map((run) => <tr key={run.id}><td><strong>{stageLabel(run.stage)}</strong><div className="muted">{run.provider} · {run.model}</div></td><td><StatusBadge value={run.status} /></td><td>{run.duration_ms ?? 0} ms</td><td>{run.usage.total_tokens ?? "-"}</td></tr>)}
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
					<div><div className="toolbar"><StatusBadge value={latest.status} /><strong>{stageLabel(latest.stage)}</strong><span className="muted">{latest.id} · revision {latest.input_revision || "-"} · {latest.progress}%</span></div>{(latest.error || latest.message) && <p className={latest.error ? "error-text" : "muted"}>{latest.error || latest.message}</p>}</div>
				) : <p className="muted">No pipeline jobs have been recorded yet.</p>}
			</Panel>
		</div>
	);
}

function stageLabel(stage: string) {
	return stage.replace(/_/g, " ").replace(/\b\w/g, (letter: string) => letter.toUpperCase());
}

function SourceProcessingFlow() {
	const stages = [
		{ icon: FileCheck2, title: "Input resources", detail: "Extracted text and physical lines", tone: "Deterministic" },
		{ icon: Sparkles, title: "Sentence segmentation", detail: "LLM groups backend-owned exact spans", tone: "LLM-assisted" },
		{ icon: ScanText, title: "Fidelity validation", detail: "Backend rebuilds text and verifies complete coverage", tone: "Deterministic" },
		{ icon: Sparkles, title: "Source classification", detail: "Kind, relevance, confidence and warnings", tone: "LLM-assisted" },
		{ icon: Languages, title: "Normalization & review", detail: "Backend-owned text and auditable decisions", tone: "Backend + human" },
	];
	return (
		<section className="source-flow" aria-label="Source processing flow">
			{stages.map((stage, index) => {
				const Icon = stage.icon;
				return (
					<div className="source-flow-stage" key={stage.title}>
						<div className="source-flow-heading"><Icon size={18} /><strong>{stage.title}</strong></div>
						<span>{stage.detail}</span>
						<Badge tone={stage.tone === "LLM-assisted" ? "warn" : "good"}>{stage.tone}</Badge>
						{index < stages.length - 1 && <span className="source-flow-arrow" aria-hidden="true">→</span>}
					</div>
				);
			})}
		</section>
	);
}

function SourceFidelityPanel({ report }: { report: SourceFidelityReport }) {
	const coverage = Math.round(report.normative_coverage * 100);
	return (
		<Panel title="Source fidelity gate">
			<div className="grid-3 compact-metrics">
				<Metric label="Physical source segments" value={report.segments_total} />
				<Metric label="Normative segments covered" value={`${report.normative_covered}/${report.normative_segments}`} />
				<Metric label="Normative coverage" value={`${coverage}%`} />
			</div>
			<div className="fidelity-coverage" aria-label={`${coverage}% normative source coverage`}>
				<div className="progress-track"><div className="progress-bar" style={{ width: `${coverage}%` }} /></div>
				<div className="toolbar">
					<Badge tone={report.ok ? "good" : "bad"}>{report.ok ? "Lossless coverage passed" : "Coverage blocked"}</Badge>
					<Badge>pipeline {report.pipeline_version}</Badge>
					{report.needs_attention.length > 0 && <Badge tone="warn">{report.needs_attention.length} need attention</Badge>}
				</div>
			</div>
			<p className="muted">Physical lines remain the lossless fidelity layer. The backend creates exact candidate spans, the LLM only groups their IDs, and the backend verifies that no normative source span was lost.</p>
			{report.errors.length > 0 && <div className="error-text">{report.errors.join(" ")}</div>}
		</Panel>
	);
}

function CombinedSentenceList({ items }: { items: CombinedDocumentSentence[] }) {
	if (!items.length) return <p className="muted">No OD units generated.</p>;
	return (
		<div className="od-sentence-list">
			{items.map((item) => (
				<article className="od-sentence-card" key={item.id}>
					<div className="toolbar">
						<strong>{item.id}</strong>
						<Badge>{item.kind ?? "sentence"}</Badge>
						{item.role && <Badge>{item.role}</Badge>}
						<Badge tone={item.transformation === "merged" ? "warn" : "default"}>{item.transformation}</Badge>
						<Badge tone={item.confidence === "low" ? "warn" : "good"}>{item.confidence}</Badge>
					</div>
					<p>{item.text}</p>
					<div className="muted">{item.derived_from.map((origin) => `${origin.resource_id} · lines ${origin.line_start}–${origin.line_end}${origin.source_segment_id ? ` · ${origin.source_segment_id} bytes ${origin.start_byte ?? 0}–${origin.end_byte ?? 0}` : ""}`).join(", ")}</div>
					{item.warnings.length > 0 && <div className="error-text">{item.warnings.join(" ")}</div>}
				</article>
			))}
		</div>
	);
}

function SegmentationGroupList({ proposal }: { proposal: SourceSegmentationProposal }) {
	const candidates = new Map(proposal.candidates.map((candidate) => [candidate.id, candidate]));
	if (!proposal.groups.length) return <p className="muted">No segmentation groups generated.</p>;
	return (
		<div className="od-sentence-list">
			{proposal.groups.map((group) => {
				const members = group.candidate_ids.map((id) => candidates.get(id)).filter((item) => item !== undefined);
				return (
					<article className="od-sentence-card" key={group.id}>
						<div className="toolbar">
							<strong>{group.id}</strong>
							<Badge tone={group.role === "layout_noise" ? "warn" : "default"}>{group.role}</Badge>
							<Badge tone={group.requires_review ? "warn" : "good"}>{group.confidence}</Badge>
							{group.od_sentence_id && <Badge>{group.od_sentence_id}</Badge>}
						</div>
						<p>{members.map((member) => member?.exact_text).join(" ")}</p>
						<div className="muted">{members.map((member) => `${member?.id} · ${member?.segment_id} · lines ${member?.line_start}–${member?.line_end} · bytes ${member?.start_byte}–${member?.end_byte}`).join(", ")}</div>
						{group.warnings.length > 0 && <div className="error-text">{group.warnings.join(" ")}</div>}
					</article>
				);
			})}
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
	const [sourceJob, setSourceJob] = useState<Job | null>(null);
	const [segmentationJob, setSegmentationJob] = useState<Job | null>(null);
	const project = useQuery({ queryKey: ["project", projectId], queryFn: () => api.getProject(projectId) });
	const sourceQA = useQuery({
		queryKey: ["source-unit-qa", projectId],
		queryFn: () => api.sourceUnitQA(projectId),
		retry: false,
	});
	const generateSourceUnits = useMutation({
		mutationFn: () => api.generateSourceUnits(projectId, project.data?.project.current_revision ?? 0),
		onSuccess: ({ job }) => setSourceJob(job),
	});
	const retrySegmentation = useMutation({
		mutationFn: () => api.processSources(projectId, project.data?.project.current_revision ?? 0),
		onSuccess: ({ job }) => setSegmentationJob(job),
	});
  const resources = useQuery({ queryKey: ["resources", projectId], queryFn: () => api.listResources(projectId) });
	const sourceManifest = useQuery({ queryKey: ["source-manifest", projectId], queryFn: () => api.sourceManifest(projectId) });
	const sourceFidelity = useQuery({ queryKey: ["source-fidelity", projectId], queryFn: () => api.sourceFidelity(projectId), retry: false });
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
		<SourceProcessingFlow />
      <div className="grid-3">
        <Metric label="Resources" value={sourceManifest.data?.manifest.summary.total ?? resources.data?.items.length ?? "-"} />
        <Metric label="Ready resources" value={sourceManifest.data?.manifest.summary.ready ?? "-"} />
        <Metric label="OD sentences" value={combinedDocument.data?.combined_document.summary.sentence_count ?? 0} />
      </div>
		{sourceFidelity.data?.source_fidelity && <SourceFidelityPanel report={sourceFidelity.data.source_fidelity} />}
		{segmentationJob && (
			<Panel title="Sentence segmentation">
				<JobProgress
					projectId={projectId}
					job={segmentationJob}
					onDone={() => {
						setSegmentationJob(null);
						void queryClient.invalidateQueries({ queryKey: ["project", projectId] });
						void queryClient.invalidateQueries({ queryKey: ["source-segmentation", projectId] });
						void queryClient.invalidateQueries({ queryKey: ["source-fidelity", projectId] });
						void queryClient.invalidateQueries({ queryKey: ["combined-document", projectId] });
						void queryClient.invalidateQueries({ queryKey: ["source-units", projectId] });
					}}
				/>
			</Panel>
		)}
		{sourceJob && (
			<Panel title="Source-unit classification">
				<JobProgress
					projectId={projectId}
					job={sourceJob}
					onDone={() => {
						setSourceJob(null);
						void queryClient.invalidateQueries({ queryKey: ["project", projectId] });
						void queryClient.invalidateQueries({ queryKey: ["source-units", projectId] });
						void queryClient.invalidateQueries({ queryKey: ["source-unit-qa", projectId] });
					}}
				/>
			</Panel>
		)}
      <div className="grid-2">
        <Panel title="Input Resources">
          {resources.isLoading ? <LoadingState /> : <ResourceSummaryTable items={resources.data?.items ?? []} />}
        </Panel>
		<Panel
			title="Validated Combined Document"
			action={
				<div className="toolbar">
					<Button className={combinedView === "semantic" ? "active" : ""} onClick={() => setCombinedView("semantic")}>Sentences</Button>
					<Button className={combinedView === "layout" ? "active" : ""} onClick={() => setCombinedView("layout")}>Segmentation audit</Button>
					<Button className={combinedView === "raw" ? "active" : ""} onClick={() => setCombinedView("raw")}>Raw</Button>
					<Button
						variant="primary"
						disabled={!combinedDocument.data?.combined_document || generateSourceUnits.isPending || !!sourceJob}
						onClick={() => generateSourceUnits.mutate()}
					>
						Classify Source Units
					</Button>
				</div>
			}
		>
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
			{combinedDocument.data.combined_document.summary.fallback_used && (
				<div className="review-banner">
					<div>
						<strong>LLM segmentation was not applied.</strong>
						<p className="muted">Processing completed safely with deterministic sentence splitting. You can continue or retry when the LLM is available.</p>
					</div>
					<Button disabled={retrySegmentation.isPending || !!segmentationJob} onClick={() => retrySegmentation.mutate()}>Retry with LLM</Button>
				</div>
			)}
				{combinedView === "raw" ? (
					<pre>{combinedDocument.data.combined_document.markdown}</pre>
				) : combinedView === "layout" ? (
					sourceSegmentation.data?.proposal ? <SegmentationGroupList proposal={sourceSegmentation.data.proposal} /> : <p className="muted">Segmentation audit is unavailable for this legacy project. Reprocess sources to generate it.</p>
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
						{sourceQA.data.qa.od_sentences_referenced}/{sourceQA.data.qa.od_sentences_total} OD sentences covered
					</Badge>
				</div>
				{needsAttention > 0 ? (
					<p className="muted">Open a flagged unit, inspect the backend normalization audit, then accept the classification or exclude the unit from modeling.</p>
				) : (
					<p className="muted">All source-unit review gates are resolved. Requirement extraction is available as the next pipeline step.</p>
				)}
			</Panel>
		)}
        <Panel
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
					<strong>Why this needs attention</strong>
					<ul>{unit.warnings?.map((warning) => <li key={warning}>{warning}</li>)}</ul>
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
              <th style={{ width: 130 }}>Atom</th>
              <th>Statement</th>
              <th>Area</th>
              <th>Outcome</th>
              <th>Review</th>
            </tr>
          </thead>
          <tbody>
            {(requirements.data?.items ?? []).map((item) => (
              <tr key={item.id} onClick={() => setSelected(item)}>
                <td>
                  <strong>{item.id}</strong>
                </td>
                <td>{item.statement}</td>
                <td>{item.functional_area}</td>
                <td>{item.modeling_outcome}</td>
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
				<table className="data-table"><thead><tr><th>Obligation</th><th>Kind</th><th>Persistence</th><th>Risk</th></tr></thead><tbody>
					{obligations.data.design_obligations.map((item) => <tr key={item.id}><td><strong>{item.id}</strong><div>{item.statement}</div></td><td>{item.kind}</td><td>{item.persistence}</td><td><StatusBadge value={item.risk} /></td></tr>)}
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
          </div>
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
		<Panel title="Actors">
			{actors.isLoading ? <LoadingState /> : (
				<div className="toolbar">
					{(actors.data?.items ?? []).map((actor) => <Badge key={actor.id}>{actor.label} · {actor.kind}</Badge>)}
				</div>
			)}
		</Panel>
		<Panel title={`CRUD Operations · ${filteredOperations.length}`}>
			{operations.isLoading ? <LoadingState /> : filteredOperations.length > 0 ? (
				<CrudTable items={filteredOperations} />
			) : (
				<p className="muted">CRUD operations are generated by the next pipeline stage, Build CRUD Mapping. The functional-analysis items are shown above.</p>
			)}
		</Panel>
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
					<th style={{ width: 100 }}>Atom</th>
					<th>Requirement</th>
					{showArea && <th>Area</th>}
					<th>Type</th>
					<th>Relevance</th>
					<th>Source units</th>
				</tr>
			</thead>
			<tbody>
				{items.map((item) => (
					<tr key={item.id}>
						<td><strong>{item.id}</strong></td>
						<td>{item.statement}</td>
						{showArea && <td>{areaByAtom.get(item.id)?.label ?? "Unmapped"}</td>}
						<td>{item.atom_type.replace(/_/g, " ")}</td>
						<td><StatusBadge value={item.modeling_relevance} /></td>
						<td>{item.source_units.join(", ")}</td>
					</tr>
				))}
			</tbody>
		</table>
	);
}

function CrudTable({ items }: { items: CrudOperation[] }) {
  return (
    <table className="data-table">
      <thead>
        <tr>
          <th>Operation</th>
          <th>Actor</th>
          <th>C</th>
          <th>R</th>
          <th>U</th>
          <th>D</th>
          <th>Outcome</th>
        </tr>
      </thead>
      <tbody>
        {items.map((item) => (
          <tr key={item.id}>
            <td>
              <strong>{item.label}</strong>
              <div className="muted">{item.id}</div>
            </td>
            <td>{item.actor_id}</td>
            <td>{item.creates.length}</td>
            <td>{item.reads.length}</td>
            <td>{item.updates.length}</td>
            <td>{item.deletes.length}</td>
            <td>
              <StatusBadge value={item.outcome} />
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
	nextDisabled,
}: {
	projectId: string;
	revision: number;
	nextAction: string;
	onNext: () => void;
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
            void queryClient.invalidateQueries();
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
