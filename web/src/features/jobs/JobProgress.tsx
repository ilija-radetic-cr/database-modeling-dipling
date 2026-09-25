import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Circle, Loader2, RotateCcw, XCircle } from "lucide-react";
import type { Job, JobEvent } from "@/shared/api/types";
	import { api, subscribeToJob } from "@/shared/api/client";
	import { Button, StatusBadge } from "@/shared/components/ui";
	import { canRetryJob, isTerminalJobStatus, requiresLogicalValidationRepair } from "@/shared/lib/pipeline";

export function JobProgress({
  projectId,
  job,
  onDone,
  onDismiss,
}: {
  projectId: string;
  job: Job;
  onDone?: () => void;
  onDismiss?: (job: Job) => void;
}) {
  const queryClient = useQueryClient();
	const [activeJob, setActiveJob] = useState(job);
  const [event, setEvent] = useState<JobEvent | null>(null);
	const [events, setEvents] = useState<JobEvent[]>([]);
	const onDoneRef = useRef(onDone);
	const completedNotificationRef = useRef<string | null>(null);
	onDoneRef.current = onDone;
	const polledJob = useQuery({
		queryKey: ["job", projectId, activeJob.id],
		queryFn: () => api.job(projectId, activeJob.id),
		initialData: { job: activeJob },
		refetchInterval: (query) => isTerminalJobStatus(query.state.data?.job.status ?? activeJob.status) ? false : 1500,
		refetchOnWindowFocus: true,
	});
	const currentJob = polledJob.data?.job ?? activeJob;
	const currentStatus = event?.status ?? currentJob.status;
	const message = event?.message ?? currentJob.message ?? "Job queued.";
	const error = currentJob.error ?? (currentStatus === "failed" ? message : "");
	const logicalValidationFailure = requiresLogicalValidationRepair(currentJob);
	const project = useQuery({
		queryKey: ["project", projectId],
		queryFn: () => api.getProject(projectId),
		enabled: canRetryJob(currentStatus),
	});
	const retry = useMutation({
		mutationFn: () => {
			const revision = project.data?.project.current_revision ?? currentJob.input_revision ?? 0;
			return logicalValidationFailure
				? api.runStage(projectId, "logical_model", revision)
				: api.retryJob(projectId, currentJob.id, revision);
		},
		onSuccess: ({ job: retried }) => {
			setActiveJob(retried);
			setEvent(null);
			setEvents([]);
			completedNotificationRef.current = null;
		},
	});

	useEffect(() => {
		setActiveJob(job);
		setEvent(null);
		setEvents([]);
		completedNotificationRef.current = null;
	}, [job.id]);

  useEffect(() => {
    return subscribeToJob(
      projectId,
		activeJob.id,
		(nextEvent) => {
			setEvent(nextEvent);
			setEvents((current) => {
				const key = `${nextEvent.status}:${nextEvent.step ?? ""}:${nextEvent.created_at}`;
				if (current.some((item) => `${item.status}:${item.step ?? ""}:${item.created_at}` === key)) return current;
				return [...current, nextEvent];
			});
		},
		() => {
			void queryClient.invalidateQueries({ queryKey: ["job", projectId, activeJob.id] });
      },
    );
	}, [activeJob.id, projectId, queryClient]);

	useEffect(() => {
		if (currentStatus !== "completed" || completedNotificationRef.current === currentJob.id) return;
		completedNotificationRef.current = currentJob.id;
		void queryClient.invalidateQueries();
		onDoneRef.current?.();
	}, [currentJob.id, currentStatus, queryClient]);

	const progress = event?.progress ?? currentJob.progress ?? 0;
	const running = !isTerminalJobStatus(currentStatus);
	const elapsed = useElapsedSeconds(currentJob.started_at ?? currentJob.created_at, running ? undefined : currentJob.completed_at ?? currentJob.updated_at);
	const failed = canRetryJob(currentStatus);
	const timeline = events.filter((item, index) => {
		if (!item.step) return false;
		const round = item.metadata?.repair_round;
		return events.findIndex((other) => other.step === item.step && other.metadata?.repair_round === round) === index;
	});
	const lastRepair = [...events].reverse().find((item) => item.step === "repair_logical_model");
  return (
    <div className="job-progress">
		<div className="job-headline">
			{running ? <Loader2 size={18} className="spin" /> : <StatusBadge value={currentStatus} />}
			<div>
				<strong>{stageDescription(currentJob.stage || currentJob.type)}</strong>
				<div className="muted small">{currentJob.id} · attempt {currentJob.attempt || 1} · revision {currentJob.input_revision || "current"}</div>
			</div>
			<span className="job-elapsed">{formatElapsed(elapsed)}</span>
		</div>
      <div className="progress-track">
        <div className={`progress-bar ${failed ? "failed" : ""} ${running ? "running" : ""}`} style={{ width: `${progress}%` }} />
      </div>
		{error ? (
			<div className="job-error">
				<strong>{friendlyError(error)}</strong>
				<details><summary>Technical details</summary><code>{error}</code></details>
			</div>
		) : (
			<p className="muted">{message}</p>
		)}
		{logicalValidationFailure && (
			<p className="muted">
				Automatic in-job repair has finished. Retry starts from the preserved proposal and exact remaining validation errors.
			</p>
		)}
		{lastRepair?.metadata?.repair_round && (
			<p className={lastRepair.metadata.stalled ? "error-text" : "muted"}>
				Repair round {lastRepair.metadata.repair_round}/{lastRepair.metadata.max_repair_attempts ?? "?"}: {lastRepair.metadata.validation_errors ?? 0} validation issue(s)
				{lastRepair.metadata.stalled ? " · stopped because the error set did not change" : ""}.
			</p>
		)}
		{polledJob.isError && <p className="error-text">Live status polling failed. Reopen Activity &amp; LLM Runs to inspect the persisted job.</p>}
		{timeline.length > 0 && (
			<ol className="job-timeline">
				{timeline.map((item) => (
					<li className={item.step === event?.step ? "active" : ""} key={`${item.step}:${item.created_at}`}>
						{item.status === "failed" ? <XCircle size={15} /> : item.step === event?.step && event?.status === "running" ? <Circle size={15} /> : <CheckCircle2 size={15} />}
						{humanizeStep(item.step!)}{item.metadata?.repair_round ? ` ${item.metadata.repair_round}/${item.metadata.max_repair_attempts ?? "?"}` : ""}
					</li>
				))}
			</ol>
		)}
		{failed && (
			<div className="toolbar">
				<Button onClick={() => retry.mutate()} disabled={retry.isPending || project.isLoading}>
					<RotateCcw size={16} /> {logicalValidationFailure ? "Repair validation errors" : "Retry against latest revision"}
				</Button>
				{onDismiss && <Button variant="ghost" onClick={() => onDismiss(currentJob)}>Dismiss and run stage again</Button>}
				{retry.isError && <span className="error-text">{retry.error instanceof Error ? retry.error.message : "Retry unavailable."}</span>}
			</div>
		)}
    </div>
  );
}

const stageDescriptions: Record<string, string> = {
	process_sources: "LLM produces complete evidence units · backend assigns canonical SU IDs and validates the contract",
	combined_document: "LLM produces complete evidence units · backend assigns canonical SU IDs and validates the contract",
	requirement_atoms: "LLM extracts atomic requirements · backend derives design obligations",
	functional_analysis: "LLM groups requirements into actors and functional areas",
	crud_mapping: "LLM maps business operations to create / read / update / delete effects",
	review_candidates: "Finding design questions that need a human decision",
	apply_review_decision_batch: "Applying your decisions deterministically (no LLM)",
	apply_review_decision: "Applying your decision",
	conceptual_model: "LLM describes what the system must remember · backend derives the conceptual model and checks coverage",
	logical_model: "Deterministic mapping of the conceptual model into DB-DSL (no LLM) · full validation",
	semantic_verification: "Verifying that every design obligation is realized (no LLM)",
	generate_outputs: "Generating DBML and the traceability report (deterministic)",
	validation_lint: "Validating and linting the DB-DSL model",
};

// friendlyError turns provider and pipeline failures into an actionable sentence;
// the raw message stays available under "Technical details".
export function friendlyError(error: string) {
	const text = error.toLowerCase();
	if (text.includes("no credits") || text.includes("insufficient_quota") || text.includes("exceeded your current quota")) {
		return "The OpenAI account has no credits left. Add credits in the OpenAI billing settings, then retry this step.";
	}
	if (text.includes("429") || text.includes("rate limit")) return "The LLM provider is rate-limiting requests. Wait a moment and retry.";
	if (text.includes("deadline exceeded") || text.includes("timeout")) return "The LLM call took too long and was stopped. Retry this step.";
	if (text.includes("api key") || text.includes("openai_api_key") || text.includes("llm client is required")) return "No LLM API key is configured on the server.";
	if (text.includes("revision_conflict") || text.includes("revision conflict")) return "The project changed while this step was running. Retry against the latest revision.";
	if (text.includes("need your decision before conceptual modeling")) return "Some requirements still need your decision. They were added to the Review Queue; answer them and the pipeline continues.";
	if (text.includes("cannot be mapped deterministically")) return "The conceptual model is incomplete for database mapping. Review the conceptual model and resolve the listed items.";
	return "This step failed. You can retry it; completed earlier steps are kept unchanged.";
}

function stageDescription(stage: string) {
	return stageDescriptions[stage] ?? humanizeStep(stage);
}

function useElapsedSeconds(from?: string, until?: string) {
	const [now, setNow] = useState(() => Date.now());
	useEffect(() => {
		if (until) return;
		const timer = window.setInterval(() => setNow(Date.now()), 1000);
		return () => window.clearInterval(timer);
	}, [until]);
	const start = from ? Date.parse(from) : NaN;
	if (Number.isNaN(start)) return 0;
	const end = until ? Date.parse(until) : now;
	return Math.max(0, Math.round(((Number.isNaN(end) ? now : end) - start) / 1000));
}

function formatElapsed(seconds: number) {
	return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
}

function humanizeStep(step: string) {
	return step.replace(/_/g, " ").replace(/\b\w/g, (letter: string) => letter.toUpperCase());
}
