import { describe, expect, it } from "vite-plus/test";
import { classesFor, stylesheet } from "./marks";

describe("classesFor", () => {
  it("marks action status", () => {
    expect(classesFor("Action", "rerun_changed", { tool: "triage" })).toContain("st-rerun_changed");
    expect(classesFor("Action", "rerun_changed", { tool: "triage" })).toContain("kind-action");
  });
  it("marks inactive facts", () => {
    expect(classesFor("Fact", undefined, { active: false })).toContain("inactive");
    expect(classesFor("Fact", undefined, { active: true })).not.toContain("inactive");
  });
  it("marks undone effects", () => {
    expect(classesFor("Effect", undefined, { sent: true, live: false })).toContain("undone");
    expect(classesFor("Effect", undefined, { sent: true, live: true })).not.toContain("undone");
  });
});

describe("stylesheet", () => {
  const sheet = stylesheet() as { selector: string; style: Record<string, unknown> }[];
  it("styles every status", () => {
    for (const st of [
      "ok",
      "poisoned",
      "rerun_changed",
      "rerun_same",
      "skipped",
      "superseded",
      "new",
      "pending",
      "running",
    ]) {
      expect(sheet.some((b) => b.selector.includes(`.st-${st}`))).toBe(true);
    }
  });
  it("uses gold only for repaired marks", () => {
    const allowed = ["st-rerun_changed", "st-new", "st-running", "on-path", "repaired"];
    for (const b of sheet) {
      const gold = Object.entries(b.style).some(
        ([k, v]) =>
          k !== "background-image" && typeof v === "string" && /f5a623|245,\s*166,\s*35/i.test(v),
      );
      if (gold)
        expect(
          allowed.some((a) => b.selector.includes(a)),
          b.selector,
        ).toBe(true);
    }
  });
});
