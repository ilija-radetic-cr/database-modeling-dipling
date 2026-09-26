import { describe, expect, it } from "vitest";
import { manualFindingInput, type ManualFindingDraft } from "./manualFinding";

const completeDraft: ManualFindingDraft = {
	severity: "error",
	category: "missing",
	sourceUnitIDs: "SU-014, SU-014 SU-015",
	descriptionRefs: "korisnik, korisnik.email\nkorisnik.email",
	sourceQuote: "  Exact source text.  ",
	claim: " Missing comparison semantics. ",
	expected: " Case-insensitive uniqueness. ",
	actual: " Plain uniqueness. ",
	suggestedCorrection: " Preserve comparison mode. ",
	note: " TEST-OPERATOR: manually verified. ",
};

describe("manual adversarial finding", () => {
	it("builds a trimmed, de-duplicated backend input", () => {
		expect(manualFindingInput(completeDraft)).toEqual({
			note: "TEST-OPERATOR: manually verified.",
			finding: {
				severity: "error",
				category: "missing",
				source_unit_ids: ["SU-014", "SU-015"],
				description_refs: ["korisnik", "korisnik.email"],
				source_quote: "Exact source text.",
				claim: "Missing comparison semantics.",
				expected: "Case-insensitive uniqueness.",
				actual: "Plain uniqueness.",
				suggested_correction: "Preserve comparison mode.",
			},
		});
	});

	it("rejects an incomplete finding before any request is made", () => {
		expect(manualFindingInput({ ...completeDraft, note: "" })).toBeNull();
		expect(manualFindingInput({ ...completeDraft, sourceUnitIDs: "" })).toBeNull();
		expect(manualFindingInput({ ...completeDraft, category: "contradiction", descriptionRefs: "" })).toBeNull();
		expect(manualFindingInput({ ...completeDraft, sourceQuote: "" })).toBeNull();
	});

	it("allows a missing-element finding without a description reference", () => {
		const input = manualFindingInput({ ...completeDraft, category: "missing", descriptionRefs: "" });
		expect(input?.finding.description_refs).toEqual([]);
	});
});
