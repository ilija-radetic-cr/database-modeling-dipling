import { describe, expect, it } from "vitest";
import { humanizeStatus, statusTone } from "./status";

describe("status helpers", () => {
  it("humanizes API statuses", () => {
    expect(humanizeStatus("analysis_review")).toBe("Needs decisions");
    expect(humanizeStatus("some_new_value")).toBe("Some new value");
  });

  it("maps blocking statuses to bad tone", () => {
    expect(statusTone("error")).toBe("bad");
    expect(statusTone("completed")).toBe("good");
    expect(statusTone("open_review")).toBe("warn");
  });
});
