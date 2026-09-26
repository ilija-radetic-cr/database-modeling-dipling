import type { AdversarialReviewFindingInput } from "@/shared/api/types";

export type ManualFindingDraft = {
	severity: "error" | "warning";
	category: "missing" | "contradiction" | "cardinality" | "ambiguity" | "unsupported" | "physical_gap";
	sourceUnitIDs: string;
	descriptionRefs: string;
	sourceQuote: string;
	claim: string;
	expected: string;
	actual: string;
	suggestedCorrection: string;
	note: string;
};

export const emptyManualFinding: ManualFindingDraft = {
	severity: "error",
	category: "missing",
	sourceUnitIDs: "",
	descriptionRefs: "",
	sourceQuote: "",
	claim: "",
	expected: "",
	actual: "",
	suggestedCorrection: "",
	note: "",
};

function parseIDs(value: string, separator = /[\s,]+/) {
	return [...new Set(value.split(separator).map((id) => id.trim()).filter(Boolean))];
}

export function manualFindingInput(draft: ManualFindingDraft): { finding: AdversarialReviewFindingInput; note: string } | null {
	const sourceUnitIDs = parseIDs(draft.sourceUnitIDs);
	// A reference may name a property written with spaces, so only commas and
	// line breaks separate references.
	const descriptionRefs = parseIDs(draft.descriptionRefs, /[,\n]+/);
	const sourceQuote = draft.sourceQuote.trim();
	const claim = draft.claim.trim();
	const expected = draft.expected.trim();
	const actual = draft.actual.trim();
	const suggestedCorrection = draft.suggestedCorrection.trim();
	const note = draft.note.trim();
	if (!sourceUnitIDs.length || (draft.category !== "missing" && !descriptionRefs.length) || !sourceQuote || !claim || !expected || !actual || !suggestedCorrection || !note) return null;
	return {
		note,
		finding: {
			severity: draft.severity,
			category: draft.category,
			source_unit_ids: sourceUnitIDs,
			description_refs: descriptionRefs,
			source_quote: sourceQuote,
			claim,
			expected,
			actual,
			suggested_correction: suggestedCorrection,
		},
	};
}
