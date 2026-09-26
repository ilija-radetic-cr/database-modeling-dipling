import type { AdversarialReviewDecision, AdversarialReviewResponse } from "@/shared/api/types";

export function latestReviewDecisions(
	response: Pick<AdversarialReviewResponse, "review" | "decisions" | "finding_provenance"> | undefined,
) {
	const latestReviewID = response?.review?.review_id;
	const latest = new Map<string, AdversarialReviewDecision>();
	if (!latestReviewID || !response) return latest;
	const originReviewByFinding = new Map(response.finding_provenance.map((item) => [item.finding_id, item.review_id]));
	for (const decision of response.decisions) {
		const originReviewID = originReviewByFinding.get(decision.finding_id) ?? latestReviewID;
		if (decision.review_id === originReviewID) latest.set(decision.finding_id, decision);
	}
	return latest;
}
