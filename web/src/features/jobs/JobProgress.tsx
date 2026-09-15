import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Circle, RotateCcw, XCircle } from "lucide-react";
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
	const failed = canRetryJob(currentStatus);
	const timeline = events.filter((item, index) => item.step && events.findIndex((other) => other.step === item.step) === index);
  return (
    <div className="job-progress">
		<div className="toolbar">
			<StatusBadge value={currentStatus} />
			<span className="muted">{currentJob.id} · attempt {currentJob.attempt || 1} · input revision {currentJob.input_revision || "current"}</span>
		</div>
      <div className="progress-track">
        <div className="progress-bar" style={{ width: `${progress}%` }} />
      </div>
		<p className={error ? "error-text" : "muted"}>{error || message}</p>
		{logicalValidationFailure && (
			<p className="muted">
				The failed candidate is preserved. Repair starts a new job with the exact validation errors and the previous proposal.
			</p>
		)}
		{polledJob.isError && <p className="error-text">Live status polling failed. Reopen Activity &amp; LLM Runs to inspect the persisted job.</p>}
		{timeline.length > 0 && (
			<ol className="job-timeline">
				{timeline.map((item) => (
					<li className={item.step === event?.step ? "active" : ""} key={`${item.step}:${item.created_at}`}>
						{item.status === "failed" ? <XCircle size={15} /> : item.step === event?.step && event?.status === "running" ? <Circle size={15} /> : <CheckCircle2 size={15} />}
						{humanizeStep(item.step!)}
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

function humanizeStep(step: string) {
	return step.replace(/_/g, " ").replace(/\b\w/g, (letter: string) => letter.toUpperCase());
}
