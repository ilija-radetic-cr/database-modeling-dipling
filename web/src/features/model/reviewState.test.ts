import { describe, expect, it } from "vitest";
import type { AdversarialReviewDecision, AdversarialReviewRecord, AdversarialReviewResponse } from "@/shared/api/types";
import { latestReviewDecisions } from "./reviewState";

function decision(reviewID: string, findingID: string, value: AdversarialReviewDecision["decision"]): AdversarialReviewDecision {
	return {
		review_id: reviewID,
		finding_id: findingID,
		decision: value,
		note: `${reviewID}:${findingID}:${value}`,
		actor: "test_operator",
		decided_at: "2026-09-25T20:00:00Z",
		project_revision: 12,
	};
}

function review(reviewID: string): AdversarialReviewRecord {
	return {
		review_id: reviewID,
		scope: "conceptual",
		candidate_revision: 12,
		candidate_hash: "sha256:candidate",
		source_hash: "sha256:source",
		summary: "Test review.",
		findings: [],
	};
}

describe("latestReviewDecisions", () => {
	it("uses each finding's immutable origin review and ignores unrelated same-ID history", () => {
		const response = {
			review: review("review-current"),
			finding_provenance: [
				{ finding_id: "F-LLM", review_id: "review-current", origin: "llm", actor: "adversarial_reviewer", created_at: "2026-09-25T20:00:00Z", project_revision: 12 },
				{ finding_id: "OF-001", review_id: "review-operator-origin", origin: "operator", actor: "test_operator", created_at: "2026-09-25T20:00:00Z", project_revision: 11 },
			],
			decisions: [
				decision("review-old-unrelated", "F-LLM", "waive"),
				decision("review-current", "F-LLM", "dismiss"),
				decision("review-old-unrelated", "OF-001", "dismiss"),
				decision("review-operator-origin", "OF-001", "request_correction"),
			],
		} as Pick<AdversarialReviewResponse, "review" | "decisions" | "finding_provenance">;

		const latest = latestReviewDecisions(response);
		expect(latest.get("F-LLM")?.decision).toBe("dismiss");
		expect(latest.get("OF-001")?.decision).toBe("request_correction");
		expect(latest.get("OF-001")?.review_id).toBe("review-operator-origin");
	});

	it("falls back to the current review ID for older responses without provenance", () => {
		const response = {
			review: review("review-current"),
			finding_provenance: [],
			decisions: [
				decision("review-old", "F-001", "waive"),
				decision("review-current", "F-001", "dismiss"),
			],
		} as Pick<AdversarialReviewResponse, "review" | "decisions" | "finding_provenance">;

		expect(latestReviewDecisions(response).get("F-001")?.decision).toBe("dismiss");
	});
});
