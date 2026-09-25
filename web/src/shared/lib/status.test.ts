import { describe, expect, it } from "vitest";
import { humanizeStatus, statusTone } from "./status";

describe("status helpers", () => {
  it("humanizes API statuses", () => {
    expect(humanizeStatus("source_review")).toBe("Source review");
    expect(humanizeStatus("conceptual_review")).toBe("Conceptual review");
    expect(humanizeStatus("non_model")).toBe("Non-model");
    expect(humanizeStatus("some_new_value")).toBe("Some new value");
  });

  it("maps blocking statuses to bad tone", () => {
    expect(statusTone("error")).toBe("bad");
    expect(statusTone("failed")).toBe("bad");
    expect(statusTone("completed")).toBe("good");
    expect(statusTone("needs_attention")).toBe("warn");
    expect(statusTone("proposed")).toBe("warn");
  });
});
