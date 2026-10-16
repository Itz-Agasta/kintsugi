import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { ApiError, apiUrl, getJSON, health, openStream, sessionId } from "./api";
import { SOURCE } from "./types";

function memStore(init: Record<string, string> = {}): Storage {
  const m = new Map(Object.entries(init));
  return {
    getItem: (k) => m.get(k) ?? null,
    setItem: (k, v) => void m.set(k, v),
    removeItem: (k) => void m.delete(k),
    clear: () => m.clear(),
    key: () => null,
    get length() {
      return m.size;
    },
  };
}

afterEach(() => vi.unstubAllGlobals());

describe("sessionId", () => {
  it("creates a 16 char id and keeps it", () => {
    const s = memStore();
    const id = sessionId(s);
    expect(id).toMatch(/^[a-z0-9]{16}$/);
    expect(sessionId(s)).toBe(id);
  });
  it("replaces an invalid stored id", () => {
    const s = memStore({ "kintsugi.session": "Bad ID!" });
    const id = sessionId(s);
    expect(id).toMatch(/^[a-z0-9]{16}$/);
    expect(s.getItem("kintsugi.session")).toBe(id);
  });
});

describe("apiUrl", () => {
  it("adds the session and encodes params", () => {
    const u = new URL(apiUrl("/api/blast", { source: SOURCE }));
    expect(u.pathname).toBe("/api/blast");
    expect(u.searchParams.get("session")).toMatch(/^[a-z0-9]{16}$/);
    expect(u.searchParams.get("source")).toBe(SOURCE);
    expect(apiUrl("/api/blast", { source: SOURCE })).toContain("partner-blog%2Fpolicy-update");
  });
  it("sends no session to /healthz or /api/cypher", () => {
    expect(apiUrl("/healthz")).not.toContain("session=");
    expect(apiUrl("/api/cypher")).not.toContain("session=");
  });
});

describe("getJSON", () => {
  it("throws ApiError with the busy message on 409", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("busy", { status: 409 })),
    );
    const err = await getJSON<never>("/api/graph").catch((e: ApiError) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(409);
    expect(err.message).toBe("busy, a run or repair is in progress");
  });
  it("uses the body text for other errors", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("unknown action\n", { status: 404 })),
    );
    const err = await getJSON<never>("/api/action", { id: "x" }).catch((e: ApiError) => e);
    expect(err.status).toBe(404);
    expect(err.message).toBe("unknown action");
  });
});

describe("health", () => {
  it("is false when fetch rejects", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new TypeError("network");
      }),
    );
    expect(await health()).toBe(false);
  });
  it("is true on 200", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("ok")),
    );
    expect(await health()).toBe(true);
  });
});

class FakeES {
  static last: FakeES;
  url: string;
  closed = false;
  onerror: (() => void) | null = null;
  handlers = new Map<string, (e: MessageEvent) => void>();
  constructor(url: string) {
    this.url = url;
    FakeES.last = this;
  }
  addEventListener(name: string, fn: (e: MessageEvent) => void) {
    this.handlers.set(name, fn);
  }
  close() {
    this.closed = true;
  }
  emit(name: string, data: unknown) {
    this.handlers.get(name)?.({ data: JSON.stringify(data) } as MessageEvent);
  }
}

describe("openStream", () => {
  it("parses events and closes on an end event", () => {
    vi.stubGlobal("EventSource", FakeES);
    const got: unknown[] = [];
    openStream(
      "/api/run/stream",
      {},
      { action: (d) => got.push(d), done: () => got.push("done") },
      ["done"],
    );
    FakeES.last.emit("action", { id: "a1" });
    expect(FakeES.last.closed).toBe(false);
    FakeES.last.emit("done", {});
    expect(got).toEqual([{ id: "a1" }, "done"]);
    expect(FakeES.last.closed).toBe(true);
  });
  it("closes on onerror and reports it", () => {
    vi.stubGlobal("EventSource", FakeES);
    const errs: unknown[] = [];
    openStream("/api/run/stream", {}, { error: (d) => errs.push(d) }, ["done"]);
    FakeES.last.onerror?.();
    expect(FakeES.last.closed).toBe(true);
    expect(errs).toEqual(["stream closed"]);
  });
  it("closes on a server error event with its message", () => {
    vi.stubGlobal("EventSource", FakeES);
    const errs: unknown[] = [];
    openStream("/api/repair/stream", {}, { error: (d) => errs.push(d) }, ["diff"]);
    FakeES.last.emit("error", { message: "no actions" });
    expect(FakeES.last.closed).toBe(true);
    expect(errs).toEqual(["no actions"]);
  });
  it("returns a close function", () => {
    vi.stubGlobal("EventSource", FakeES);
    const close = openStream("/api/run/stream", {}, {}, ["done"]);
    close();
    expect(FakeES.last.closed).toBe(true);
  });
});
