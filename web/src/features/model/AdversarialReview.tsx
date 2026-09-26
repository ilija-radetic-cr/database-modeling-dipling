import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, CheckCircle2, Plus, RefreshCw, ShieldAlert, WandSparkles } from "lucide-react";
import { api } from "@/shared/api/client";
import type {
	AdversarialReviewActor,
	AdversarialReviewDecision,
	AdversarialReviewDecisionType,
	AdversarialReviewFinding,
	AdversarialReviewResponse,
	Job,
} from "@/shared/api/types";
import { Badge, Button, Field, LoadingState, Panel, StatusBadge } from "@/shared/components/ui";
import { requestAutoRun } from "@/shared/lib/autopilot";
import { isTerminalJobStatus, shouldRecoverLatestJob } from "@/shared/lib/pipeline";
import { useRouter } from "@/shared/lib/router";
import { JobProgress } from "@/features/jobs/JobProgress";
import { emptyManualFinding, manualFindingInput, type ManualFindingDraft } from "./manualFinding";
import { latestReviewDecisions } from "./reviewState";

type FindingDraft = { decision: AdversarialReviewDecisionType | ""; note: string };

const decisionLabels: Record<AdversarialReviewDecisionType, string> = {
	dismiss: "Dismiss — the finding does not apply",
	waive: "Waive — accept the documented risk",
	request_correction: "Request correction — change the proposal",
};

export function adversarialReviewLabel(status: AdversarialReviewResponse["status"], canAccept: boolean) {
	if (status === "not_run") return "Not reviewed";
	if (status === "stale") return "Stale — review needs refresh";
	return canAccept ? "Current — ready for explicit acceptance" : "Current — decisions required";
}

function actorLabel(actor: string) {
	return actor === "test_operator" ? "Test operator (simulated user)" : actor === "human" ? "Human reviewer" : actor;
}

function shortHash(hash: string | undefined) {
	if (!hash) return "—";
	return hash.length > 16 ? `${hash.slice(0, 12)}…${hash.slice(-4)}` : hash;
}

function formatTime(value: string) {
	const date = new Date(value);
	return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function auditResultLabel(action: string, result: string) {
	return action === "correction_proposed" ? "New proposal requires review" : result.replace(/_/g, " ");
}

export function AdversarialReviewView({ projectId }: { projectId: string }) {
	const { navigate } = useRouter();
	const queryClient = useQueryClient();
	const project = useQuery({ queryKey: ["project", projectId], queryFn: () => api.getProject(projectId) });
	const conceptual = useQuery({ queryKey: ["conceptual-model", projectId], queryFn: () => api.conceptualModel(projectId), retry: false });
	const review = useQuery({ queryKey: ["adversarial-review", projectId], queryFn: () => api.adversarialReview(projectId), retry: false });
	const jobs = useQuery({
		queryKey: ["jobs", projectId],
		queryFn: () => api.jobs(projectId),
		retry: false,
		refetchInterval: (query) => query.state.data?.items.some((job) => !isTerminalJobStatus(job.status)) ? 2000 : false,
	});
	const [actor, setActor] = useState<AdversarialReviewActor | "">("");
	const [drafts, setDrafts] = useState<Record<string, FindingDraft>>({});
	const [correctionNote, setCorrectionNote] = useState("");
	const [acceptanceNote, setAcceptanceNote] = useState("");
	const [manualFinding, setManualFinding] = useState<ManualFindingDraft>(emptyManualFinding);
	const [showManualFinding, setShowManualFinding] = useState(false);
	const [activeJob, setActiveJob] = useState<Job | null>(null);
	// A failed review or correction stays visible with its reason after its
	// panel is released, so the actions are free again and the cause is known.
	const [failedJob, setFailedJob] = useState<Job | null>(null);
	const [dismissedJobIds, setDismissedJobIds] = useState<Set<string>>(() => new Set());

	const latestJob = jobs.data?.items.find((item) => ["adversarial_review", "conceptual_correction"].includes(item.stage));
	useEffect(() => {
		const revision = project.data?.project.current_revision;
		if (activeJob && revision !== undefined) {
			if (!shouldRecoverLatestJob(activeJob, revision)) setActiveJob(null);
			return;
		}
		if (!latestJob || revision === undefined || dismissedJobIds.has(latestJob.id) || !shouldRecoverLatestJob(latestJob, revision)) return;
		setActiveJob(latestJob);
	}, [activeJob, dismissedJobIds, latestJob, project.data?.project.current_revision]);

	const refreshReviewState = async () => {
		await Promise.all([
			queryClient.invalidateQueries({ queryKey: ["adversarial-review", projectId] }),
			queryClient.invalidateQueries({ queryKey: ["conceptual-model", projectId] }),
			queryClient.invalidateQueries({ queryKey: ["project", projectId] }),
			queryClient.invalidateQueries({ queryKey: ["jobs", projectId] }),
			queryClient.invalidateQueries({ queryKey: ["llm-runs", projectId] }),
		]);
	};
	const requiredActor = (): AdversarialReviewActor => {
		if (!actor) throw new Error("Choose the real decision actor before writing to the audit trail.");
		return actor;
	};

	const runReview = useMutation({
		mutationFn: async () => {
			const fresh = await queryClient.fetchQuery({ queryKey: ["project", projectId], queryFn: () => api.getProject(projectId), staleTime: 0 });
			return api.runAdversarialReview(projectId, fresh.project.current_revision, "conceptual");
		},
		onSuccess: ({ job }) => {
			setDismissedJobIds((current) => {
				const next = new Set(current);
				next.delete(job.id);
				return next;
			});
			setFailedJob(null);
			setActiveJob(job);
		},
	});
	const decide = useMutation({
		mutationFn: ({ findingId, draft }: { findingId: string; draft: FindingDraft }) => api.decideAdversarialReview(projectId, {
			base_revision: review.data?.project_revision ?? project.data?.project.current_revision ?? 0,
			actor: requiredActor(),
			decisions: [{ finding_id: findingId, decision: draft.decision as AdversarialReviewDecisionType, note: draft.note.trim() }],
		}),
		onSuccess: (data, variables) => {
			queryClient.setQueryData(["adversarial-review", projectId], data);
			setDrafts((current) => ({ ...current, [variables.findingId]: { decision: "", note: "" } }));
			void queryClient.invalidateQueries({ queryKey: ["project", projectId] });
		},
	});
	const appendFinding = useMutation({
		mutationFn: () => {
			const input = manualFindingInput(manualFinding);
			if (!input) throw new Error("Complete every manual finding field. A model ID is optional only when the category is missing.");
			return api.appendAdversarialFinding(projectId, {
				base_revision: review.data?.project_revision ?? project.data?.project.current_revision ?? 0,
				actor: requiredActor(),
				note: input.note,
				finding: input.finding,
			});
		},
		onSuccess: (data) => {
			queryClient.setQueryData(["adversarial-review", projectId], data);
			setManualFinding(emptyManualFinding);
			setShowManualFinding(false);
			void queryClient.invalidateQueries({ queryKey: ["project", projectId] });
		},
	});
	const correct = useMutation({
		mutationFn: () => api.correctAdversarialReview(projectId, {
			base_revision: review.data?.project_revision ?? project.data?.project.current_revision ?? 0,
			actor: requiredActor(),
			note: correctionNote.trim(),
		}),
		onSuccess: ({ job, project_revision }) => {
			queryClient.setQueryData(["project", projectId], (previous: typeof project.data) => previous ? {
				...previous,
				project: { ...previous.project, current_revision: project_revision },
			} : previous);
			setCorrectionNote("");
			setFailedJob(null);
			setDismissedJobIds((current) => {
				const next = new Set(current);
				next.delete(job.id);
				return next;
			});
			setActiveJob(job);
			void queryClient.invalidateQueries({ queryKey: ["adversarial-review", projectId] });
		},
	});
	const accept = useMutation({
		mutationFn: () => api.acceptConceptualModel(
			projectId,
			review.data?.project_revision ?? project.data?.project.current_revision ?? 0,
			requiredActor(),
			acceptanceNote.trim(),
		),
		onSuccess: async () => {
			await queryClient.invalidateQueries();
			requestAutoRun(projectId);
			navigate(`/projects/${projectId}/analysis/overview`);
		},
	});

	const latestDecisions = useMemo(() => latestReviewDecisions(review.data), [review.data]);
	const findingOrigins = useMemo(() => new Map((review.data?.finding_provenance ?? []).map((item) => [item.finding_id, item.origin])), [review.data?.finding_provenance]);
	const correctionRequested = [...latestDecisions.values()].some((decision) => decision.decision === "request_correction");
	const accepted = conceptual.data?.accepted ?? false;
	const statusText = review.data ? adversarialReviewLabel(review.data.status, review.data.can_accept) : "Loading review";
	const requestError = runReview.error ?? decide.error ?? appendFinding.error ?? correct.error ?? accept.error;

	if (project.isLoading || conceptual.isLoading || review.isLoading) return <LoadingState label="Loading adversarial review" />;
	if (conceptual.isError) return <Panel title="Conceptual model is not ready"><p className="muted">Generate a conceptual proposal before reviewing it against the task.</p></Panel>;
	if (review.isError) return <Panel title="Adversarial review is unavailable"><p className="error-text">{review.error instanceof Error ? review.error.message : "The review state could not be loaded."}</p></Panel>;

	// A completed project is a locked snapshot: its review can be read, but every
	// action below is disabled; reopening creates a new project to change it.
	const locked = project.data?.project.lifecycle_status === "completed";

	return (
		<fieldset className="page locked-scope" disabled={locked}>
			{locked && <p className="review-state-note warn"><AlertTriangle size={17} />This project is completed and locked. Reopen it from the Completed page to change the model.</p>}
			<Panel title="Review control" action={accepted ? <StatusBadge value="accepted" /> : <Badge tone="warn">{statusText}</Badge>}>
				<div className="review-control-grid">
					<div>
						<strong>Adversarial review against source evidence</strong>
						<p className="muted">A separate model challenges the conceptual proposal before a person can accept it. Running a review never accepts or corrects the proposal automatically.</p>
						{accepted && review.data?.status === "not_run" && <p className="review-state-note warn"><AlertTriangle size={17} />This accepted legacy or imported model has no recorded adversarial review.</p>}
					</div>
					<Field label="Decision actor">
						<select className="select" value={actor} onChange={(event) => setActor(event.target.value as AdversarialReviewActor | "")}>
							<option value="">Choose the actual actor…</option>
							<option value="human">Human reviewer</option>
							<option value="test_operator">Test operator (simulated user)</option>
						</select>
						{!actor && <span className="muted small">Required before any finding, decision, correction, or acceptance is recorded.</span>}
					</Field>
				</div>
				<div className="toolbar">
					<Button
						disabled={accepted || runReview.isPending || !!activeJob}
						onClick={() => runReview.mutate()}
					>
						{review.data?.status === "not_run" ? <ShieldAlert size={17} /> : <RefreshCw size={17} />}
						{review.data?.status === "not_run" ? "Run adversarial review" : "Re-run review against current proposal"}
					</Button>
					{review.data?.review && <Badge>candidate r{review.data.review.candidate_revision}</Badge>}
					{review.data?.review && <Badge>{review.data.review.scope}</Badge>}
					{review.data?.review && <span className="muted small">candidate {shortHash(review.data.review.candidate_hash)} · source {shortHash(review.data.review.source_hash)}</span>}
				</div>
				{requestError && <p className="error-text">{requestError instanceof Error ? requestError.message : "The review action failed."}</p>}
			</Panel>

			{activeJob && (
				<Panel title={activeJob.stage === "conceptual_correction" ? "Correcting conceptual proposal" : "Reviewing proposal against task"}>
					<JobProgress
						projectId={projectId}
						job={activeJob}
						onDone={() => { setActiveJob(null); void refreshReviewState(); }}
						onFailed={(job) => {
							setFailedJob(job);
							setActiveJob(null);
							void refreshReviewState();
						}}
						onDismiss={(job) => {
							setDismissedJobIds((current) => new Set(current).add(job.id));
							setActiveJob(null);
						}}
					/>
				</Panel>
			)}

			{failedJob && !activeJob && (
				<Panel title={failedJob.stage === "conceptual_correction" ? "Correction was not applied" : "Review did not complete"}>
					<JobProgress projectId={projectId} job={failedJob} onDismiss={() => setFailedJob(null)} />
				</Panel>
			)}

			{review.data?.status === "not_run" ? (
				<Panel title="No review recorded">
					<p className="muted">Run the adversarial review to compare each modeled claim with its cited source. Conceptual acceptance remains unavailable until the current findings are explicitly resolved.</p>
				</Panel>
			) : review.data?.review ? (
				<>
					<Panel title={`Review findings · ${review.data.review.findings.length}`} action={<Badge tone={review.data.status === "stale" ? "warn" : "default"}>{statusText}</Badge>}>
						<p>{review.data.review.summary}</p>
						{review.data.status === "stale" && <p className="review-state-note warn"><AlertTriangle size={17} />The proposal, source, or reviewer policy has changed. Re-run the review before making decisions or accepting the current model.</p>}
						{review.data.review.findings.length === 0 && <p className="muted">The review found no discrepancies. A named reviewer must still record the acceptance note below.</p>}
					</Panel>
					<div className="review-finding-list">
						{review.data.review.findings.map((finding) => (
							<FindingCard
								key={finding.id}
								finding={finding}
								origin={findingOrigins.get(finding.id) ?? "llm"}
								currentDecision={latestDecisions.get(finding.id)}
								draft={drafts[finding.id] ?? { decision: "", note: "" }}
								disabled={accepted || !actor || review.data?.status !== "current" || decide.isPending || !!activeJob}
								onDraft={(draft) => setDrafts((current) => ({ ...current, [finding.id]: draft }))}
								onSave={(draft) => decide.mutate({ findingId: finding.id, draft })}
							/>
						))}
					</div>
					{!accepted && review.data.status === "current" && (
						<Panel title="Operator-authored finding" action={<Badge tone="warn">No LLM call</Badge>}>
							<p className="muted">Add a source-grounded defect the automated critic missed. The operator, note, exact quote, and references are recorded in the audit. The finding still needs a separate disposition before correction or acceptance.</p>
							{!showManualFinding ? (
								<Button disabled={!actor || !!activeJob} onClick={() => setShowManualFinding(true)}><Plus size={17} />Add manual finding</Button>
							) : (
								<ManualFindingForm
									draft={manualFinding}
									disabled={appendFinding.isPending || !!activeJob}
									onDraft={setManualFinding}
									onCancel={() => { setManualFinding(emptyManualFinding); setShowManualFinding(false); }}
									onSave={() => appendFinding.mutate()}
								/>
							)}
						</Panel>
					)}
				</>
			) : null}

			{!accepted && review.data?.status === "current" && correctionRequested && (
				<Panel title="Request a corrected proposal" action={<Badge tone="warn">Creates a new proposal</Badge>}>
					<p className="muted">Describe the correction to apply. This starts one correction job, invalidates this review when the proposal changes, and never accepts the result.</p>
					<Field label="Correction instruction">
						<textarea className="textarea review-note" value={correctionNote} onChange={(event) => setCorrectionNote(event.target.value)} placeholder="State what must change and which evidence the correction must preserve." />
					</Field>
					<Button variant="primary" disabled={!actor || !correctionNote.trim() || correct.isPending || !!activeJob} onClick={() => correct.mutate()}><WandSparkles size={17} />Start one correction</Button>
				</Panel>
			)}

			{!accepted && (
				<Panel title="Human acceptance" action={<Badge tone="warn">{actor ? actorLabel(actor) : "Actor required"}</Badge>}>
					<p className="muted">Acceptance is enabled only for the current proposal after every finding is disposed. The actor and note are written to the audit trail.</p>
					<Field label="Acceptance reason">
						<textarea className="textarea review-note" value={acceptanceNote} onChange={(event) => setAcceptanceNote(event.target.value)} placeholder="Explain why this proposal is acceptable after reviewing the findings." />
					</Field>
					<Button
						variant="primary"
						disabled={!actor || !review.data?.can_accept || review.data.status !== "current" || !acceptanceNote.trim() || accept.isPending || !!activeJob}
						onClick={() => accept.mutate()}
					>
						<CheckCircle2 size={18} />Accept current conceptual model
					</Button>
				</Panel>
			)}
		</fieldset>
	);
}

function ManualFindingForm({
	draft,
	disabled,
	onDraft,
	onCancel,
	onSave,
}: {
	draft: ManualFindingDraft;
	disabled: boolean;
	onDraft: (draft: ManualFindingDraft) => void;
	onCancel: () => void;
	onSave: () => void;
}) {
	const ready = manualFindingInput(draft) !== null;
	return (
		<div className="manual-finding-form">
			<div className="manual-finding-row">
				<Field label="Severity">
					<select className="select" disabled={disabled} value={draft.severity} onChange={(event) => onDraft({ ...draft, severity: event.target.value as ManualFindingDraft["severity"] })}>
						<option value="error">Error</option>
						<option value="warning">Warning</option>
					</select>
				</Field>
				<Field label="Category">
					<select className="select" disabled={disabled} value={draft.category} onChange={(event) => onDraft({ ...draft, category: event.target.value as ManualFindingDraft["category"] })}>
						{["missing", "contradiction", "cardinality", "ambiguity", "unsupported", "physical_gap"].map((category) => <option key={category} value={category}>{category.replace(/_/g, " ")}</option>)}
					</select>
				</Field>
			</div>
			<div className="manual-finding-row">
				<Field label="Source unit IDs">
					<input className="input" disabled={disabled} value={draft.sourceUnitIDs} onChange={(event) => onDraft({ ...draft, sourceUnitIDs: event.target.value })} placeholder="SU-001, SU-004" />
				</Field>
				<Field label="Description references (optional for missing)">
					<input className="input" disabled={disabled} value={draft.descriptionRefs} onChange={(event) => onDraft({ ...draft, descriptionRefs: event.target.value })} placeholder="korisnik, korisnik.email, zahtev.stanje, rule:jedinstven_email" />
				</Field>
			</div>
			<Field label="Exact source quote">
				<textarea className="textarea review-note" disabled={disabled} value={draft.sourceQuote} onChange={(event) => onDraft({ ...draft, sourceQuote: event.target.value })} placeholder="Paste one exact, contiguous quote from the cited source unit." />
			</Field>
			<Field label="Finding claim">
				<input className="input" disabled={disabled} value={draft.claim} onChange={(event) => onDraft({ ...draft, claim: event.target.value })} placeholder="State the material defect." />
			</Field>
			<div className="manual-finding-row">
				<Field label="Expected from task">
					<textarea className="textarea review-note" disabled={disabled} value={draft.expected} onChange={(event) => onDraft({ ...draft, expected: event.target.value })} />
				</Field>
				<Field label="Actual proposal">
					<textarea className="textarea review-note" disabled={disabled} value={draft.actual} onChange={(event) => onDraft({ ...draft, actual: event.target.value })} />
				</Field>
			</div>
			<Field label="Suggested correction">
				<textarea className="textarea review-note" disabled={disabled} value={draft.suggestedCorrection} onChange={(event) => onDraft({ ...draft, suggestedCorrection: event.target.value })} />
			</Field>
			<Field label="Required audit note">
				<textarea className="textarea review-note" disabled={disabled} value={draft.note} onChange={(event) => onDraft({ ...draft, note: event.target.value })} placeholder="Explain why the operator is adding this finding and how it was verified." />
			</Field>
			<div className="toolbar">
				<Button disabled={disabled || !ready} onClick={onSave}><Plus size={17} />Record operator finding</Button>
				<Button variant="ghost" disabled={disabled} onClick={onCancel}>Cancel</Button>
			</div>
		</div>
	);
}

function FindingCard({
	finding,
	origin,
	currentDecision,
	draft,
	disabled,
	onDraft,
	onSave,
}: {
	finding: AdversarialReviewFinding;
	origin: string;
	currentDecision?: AdversarialReviewDecision;
	draft: FindingDraft;
	disabled: boolean;
	onDraft: (draft: FindingDraft) => void;
	onSave: (draft: FindingDraft) => void;
}) {
	const severityTone = finding.severity === "error" || finding.severity === "critical" ? "bad" : finding.severity === "warning" ? "warn" : "default";
	return (
		<article className="review-finding">
			<div className="review-finding-head">
				<div className="toolbar"><strong>{finding.id}</strong><Badge tone={severityTone}>{finding.severity}</Badge><Badge>{finding.category}</Badge><Badge tone={origin === "operator" ? "warn" : "default"}>{origin === "operator" ? "Operator-authored" : "LLM reviewer"}</Badge></div>
				{currentDecision && <Badge tone={currentDecision.decision === "request_correction" ? "warn" : "default"}>{currentDecision.decision.replace(/_/g, " ")}</Badge>}
			</div>
			<h3>{finding.claim}</h3>
			<blockquote>{finding.source_quote}</blockquote>
			<div className="finding-comparison">
				<div><span>Expected from task</span><p>{finding.expected}</p></div>
				<div><span>Actual proposal</span><p>{finding.actual}</p></div>
			</div>
			<div className="finding-correction"><strong>Suggested correction</strong><p>{finding.suggested_correction}</p></div>
			<div className="toolbar">
				{finding.source_unit_ids.map((id) => <Badge key={id}>source {id}</Badge>)}
				{finding.description_refs.map((ref) => <Badge key={ref}>description {ref}</Badge>)}
			</div>
			{currentDecision && (
				<div className="recorded-decision">
					<strong>Recorded by {actorLabel(currentDecision.actor)}</strong>
					<span className="muted">{formatTime(currentDecision.decided_at)} · revision {currentDecision.project_revision}</span>
					<p>{currentDecision.note}</p>
				</div>
			)}
			<div className="finding-decision-form">
				<Field label={currentDecision ? "Replace decision" : "Decision"}>
					<select className="select" value={draft.decision} disabled={disabled} onChange={(event) => onDraft({ ...draft, decision: event.target.value as FindingDraft["decision"] })}>
						<option value="">Choose a decision…</option>
						{Object.entries(decisionLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
					</select>
				</Field>
				<Field label="Required decision note">
					<textarea className="textarea review-note" value={draft.note} disabled={disabled} onChange={(event) => onDraft({ ...draft, note: event.target.value })} placeholder="Explain the evidence and reasoning for this decision." />
				</Field>
				<Button disabled={disabled || !draft.decision || !draft.note.trim()} onClick={() => onSave(draft)}>Record this decision</Button>
			</div>
		</article>
	);
}

export function AdversarialAuditPanel({ projectId, active = false }: { projectId: string; active?: boolean }) {
	const review = useQuery({
		queryKey: ["adversarial-review", projectId],
		queryFn: () => api.adversarialReview(projectId),
		retry: false,
		refetchInterval: active ? 2000 : false,
	});
	if (review.isLoading) return <Panel title="Adversarial review audit"><LoadingState /></Panel>;
	if (review.isError) return <Panel title="Adversarial review audit"><p className="error-text">{review.error instanceof Error ? review.error.message : "Audit history could not be loaded."}</p></Panel>;
	const events = [...(review.data?.audit ?? [])].reverse();
	return (
		<Panel title={`Adversarial review audit · ${events.length}`} action={<Badge tone={review.data?.status === "current" && review.data.can_accept ? "default" : "warn"}>{review.data ? adversarialReviewLabel(review.data.status, review.data.can_accept) : "Not reviewed"}</Badge>}>
			{events.length === 0 ? <p className="muted">No adversarial review, human decision, correction, or acceptance event has been recorded.</p> : (
				<div className="audit-event-list">
					{events.map((event) => (
						<article className="audit-event" key={event.event_id}>
							<div className="audit-event-head">
								<div className="toolbar"><strong>{event.action.replace(/_/g, " ")}</strong><Badge>{actorLabel(event.actor)}</Badge>{event.result && <span title={event.result}><Badge>{auditResultLabel(event.action, event.result)}</Badge></span>}</div>
								<span className="muted small">{formatTime(event.created_at)} · revision {event.project_revision}</span>
							</div>
							<div className="muted small">review {event.review_id || "—"}{event.finding_id ? ` · finding ${event.finding_id}` : ""}</div>
							{(event.reason || event.note) && <p>{event.reason ?? event.note}</p>}
							{(event.before_model_summary || event.after_model_summary) && (
								<div className="audit-summary-change">
									<div><span>Before</span><p>{event.before_model_summary ?? "—"}</p></div>
									<div><span>After</span><p>{event.after_model_summary ?? "—"}</p></div>
								</div>
							)}
							{event.changes && event.changes.length > 0 && (
								<details>
									<summary>Changed model paths · {event.changes.length}</summary>
									{event.action === "correction_proposed" && event.result && <p className="muted small">Recorded result: {event.result}</p>}
									<div className="audit-hashes" aria-label="Changed model paths">
										{event.changes.map((path) => <code key={path}>{path}</code>)}
									</div>
								</details>
							)}
							<div className="audit-hashes">
								<code title={event.before_candidate_hash ?? event.candidate_hash}>before {shortHash(event.before_candidate_hash ?? event.candidate_hash)}</code>
								<code title={event.after_candidate_hash ?? event.candidate_hash}>after {shortHash(event.after_candidate_hash ?? event.candidate_hash)}</code>
								<code title={event.source_hash}>source {shortHash(event.source_hash)}</code>
							</div>
						</article>
					))}
				</div>
			)}
		</Panel>
	);
}
