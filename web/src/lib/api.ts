// Every call to the kintsugi server goes through here.

const BASE: string = import.meta.env.VITE_API_URL ?? "http://localhost:8080";
const KEY = "kintsugi.session";
const VALID = /^[a-z0-9]{16}$/;
const NO_SESSION = new Set(["/healthz", "/api/cypher"]);

let fallback: string | null = null;

function newId(): string {
  const abc = "abcdefghijklmnopqrstuvwxyz0123456789";
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  return Array.from(bytes, (b) => abc[b % abc.length]).join("");
}

function defaultStore(): Storage | undefined {
  try {
    return globalThis.localStorage;
  } catch {
    return undefined;
  }
}

// sessionId returns this browser's visitor id, creating and saving one if the
// stored value is missing or invalid. Without storage it lives in memory.
export function sessionId(store: Storage | undefined = defaultStore()): string {
  try {
    const got = store?.getItem(KEY);
    if (got && VALID.test(got)) return got;
    const id = newId();
    store?.setItem(KEY, id);
    if (store) return id;
  } catch {
    // storage blocked: fall through to the in-memory id
  }
  fallback ??= newId();
  return fallback;
}

export function apiUrl(path: string, params: Record<string, string> = {}): string {
  const u = new URL(path, BASE);
  if (!NO_SESSION.has(path)) u.searchParams.set("session", sessionId());
  for (const [k, v] of Object.entries(params)) u.searchParams.set(k, v);
  return u.toString();
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function check(res: Response): Promise<Response> {
  if (res.ok) return res;
  if (res.status === 409) throw new ApiError(409, "busy, a run or repair is in progress");
  if (res.status === 503) throw new ApiError(503, "server full, try again shortly");
  const text = (await res.text()).trim();
  throw new ApiError(res.status, text || `HTTP ${res.status}`);
}

export async function getJSON<T>(path: string, params?: Record<string, string>): Promise<T> {
  const res = await check(await fetch(apiUrl(path, params)));
  return (await res.json()) as T;
}

export async function post(path: string): Promise<void> {
  await check(await fetch(apiUrl(path), { method: "POST" }));
}

export async function health(): Promise<boolean> {
  try {
    return (await fetch(apiUrl("/healthz"))).ok;
  } catch {
    return false;
  }
}

// openStream listens to a server-sent event stream. It closes the stream on
// any event named in `end`, on a server `error` event and on a dropped
// connection, so the browser never reconnects: a reconnect to the run stream
// would reset the graph and run again.
export function openStream(
  path: string,
  params: Record<string, string>,
  on: Record<string, (data: unknown) => void>,
  end: string[],
): () => void {
  const es = new EventSource(apiUrl(path, params));
  let open = true;
  const close = () => {
    open = false;
    es.close();
  };
  const fail = (message: string) => {
    if (!open) return;
    close();
    on.error?.(message);
  };
  for (const name of Object.keys(on)) {
    if (name === "error") continue;
    es.addEventListener(name, (e) => {
      if (!open) return;
      const data = JSON.parse((e as MessageEvent).data);
      if (end.includes(name)) close();
      on[name](data);
    });
  }
  // The server sends a named `error` event with a message. The browser's own
  // connection error has the same name but no data.
  es.addEventListener("error", (e) => {
    const raw = (e as MessageEvent).data;
    if (typeof raw !== "string") return fail("stream closed");
    try {
      fail(String(JSON.parse(raw).message ?? raw));
    } catch {
      fail(raw);
    }
  });
  es.onerror = () => fail("stream closed");
  return close;
}
