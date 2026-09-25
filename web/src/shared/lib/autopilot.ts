// Autopilot runs every automatic pipeline stage back to back and stops only at
// a human gate. A page that ends a gate (e.g. accepting the conceptual model)
// hands the run over to the analysis page through this one-shot flag.
const key = (projectId: string) => `dbdsl:autorun:${projectId}`;

export function requestAutoRun(projectId: string) {
	try {
		sessionStorage.setItem(key(projectId), "1");
	} catch {
		// Storage can be unavailable; the user can still press the next-action button.
	}
}

// The flag is removed after the current render pass, not immediately: React
// StrictMode renders a component twice in development, and the second render
// must see the same hand-over as the first.
export function consumeAutoRun(projectId: string) {
	try {
		const requested = sessionStorage.getItem(key(projectId)) === "1";
		if (requested) {
			setTimeout(() => {
				try {
					sessionStorage.removeItem(key(projectId));
				} catch {
					// Nothing to clean up when storage is unavailable.
				}
			}, 0);
		}
		return requested;
	} catch {
		return false;
	}
}

// The single end-of-pipeline destination: the finalization page until the
// project is completed, then the locked completed snapshot.
export function finalizePath(projectId: string, lifecycle: string | undefined) {
	return lifecycle === "completed" ? `/projects/${projectId}/completed` : `/projects/${projectId}/dbml`;
}

// Where the user is sent when the pipeline reaches a stage that needs a person.
export function gatePath(projectId: string, stage: string | undefined, lifecycle?: string) {
	switch (stage) {
		case "source_review": return `/projects/${projectId}/analysis/sources`;
		case "review_decisions": return `/projects/${projectId}/analysis/review`;
		case "conceptual_review": return `/projects/${projectId}/model/conceptual`;
		case "model_review": return `/projects/${projectId}/model/quality`;
		case "completed": return finalizePath(projectId, lifecycle);
		default: return null;
	}
}
