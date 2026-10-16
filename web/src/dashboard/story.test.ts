import { describe, expect, it } from "vite-plus/test";
import type { ActionDetail } from "@/lib/types";
import { outcome, title, why } from "./story";

const detail = (
  tool: string,
  status: string,
  output: string,
  extra: Partial<ActionDetail> = {},
): ActionDetail => ({
  action: { tool, status, output, ticket_id: "t05" },
  reads: [],
  writes: [],
  handoffs: [],
  effects: [],
  reran: null,
  ...extra,
});

describe("outcome", () => {
  it("reads triage, refund and notifier outputs", () => {
    expect(outcome("triage", '{"choice":"refund","level":"full"}')).toBe("Full refund");
    expect(outcome("triage", '{"choice":"refund","level":"partial"}')).toBe("Partial refund");
    expect(outcome("triage", '{"choice":"reply","level":"none"}')).toBe("Reply");
    expect(outcome("refund", '{"amount":90}')).toBe("Refund $90");
    expect(outcome("notifier", '{"amount":0,"level":"none","template":"reply"}')).toBe(
      "Reply email",
    );
    expect(outcome("notifier", '{"amount":90,"level":"partial","template":"refund"}')).toBe(
      "Refund email, $90",
    );
    expect(outcome("triage", "not json")).toBe("not json");
  });
});

describe("title", () => {
  it("says what changed", () => {
    const d = detail("triage", "rerun_changed", '{"choice":"reply","level":"none"}', {
      reran: { old_output: '{"choice":"refund","level":"full"}', old_hash: "a", new_hash: "b" },
    });
    expect(title(d)).toBe("Full refund became reply");
  });
  it("names the other statuses", () => {
    expect(title(detail("triage", "rerun_same", "{}"))).toBe("Same decision, cascade stopped");
    expect(title(detail("refund", "skipped", "{}"))).toBe("Skipped, nothing upstream changed");
    expect(title(detail("refund", "superseded", "{}"))).toBe("Replaced by the repair");
    expect(title(detail("notifier", "new", "{}"))).toBe("Created by the repair");
    expect(title(detail("research", "poisoned", "{}"))).toBe("Read the flagged page");
    expect(title(detail("triage", "ok", "{}"))).toBe("Untouched by the repair");
  });
});

describe("why", () => {
  it("blames a retired fact when it read one", () => {
    const d = detail("triage", "rerun_changed", "{}", {
      reads: [{ key: "vip_rules", version: 2, active: false }],
    });
    expect(why(d)).toContain("vip_rules");
    expect(why(d)).toContain("continued");
  });
  it("blames the handoff otherwise", () => {
    expect(why(detail("refund", "rerun_same", "{}"))).toContain("handed");
    expect(why(detail("refund", "rerun_same", "{}"))).toContain("stopped");
  });
});
