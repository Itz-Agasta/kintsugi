import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Canvas, useFrame, useThree } from "@react-three/fiber";
import { GizmoHelper, GizmoViewport, OrbitControls } from "@react-three/drei";
import { Bloom, EffectComposer } from "@react-three/postprocessing";
import ForceGraph from "r3f-forcegraph";
import * as THREE from "three";
import { cn } from "cn";
import { LANES } from "@/dashboard/layout";
import { labelFor } from "@/dashboard/marks";
import { statusOf, type State, type UiStatus } from "@/dashboard/state";
import { SOURCE, type GraphEdge, type GraphNode } from "@/lib/types";

// The provenance graph as a living 3D force graph: one cluster per lane,
// threads between them, glow on what matters. Same data as the flat view.

const LANE_OF = (n: GraphNode): number | null => {
  if (n.label === "Source") return 0;
  if (n.label === "Fact") return 2;
  if (n.label === "Effect") return 6;
  if (n.label !== "Action") return null;
  const i = (LANES as readonly string[]).indexOf(String(n.props.tool));
  return i < 0 ? null : i;
};

const REGION: Record<string, string> = {
  sources: "web pages read",
  research: "agent, writes facts",
  facts: "shared memory",
  triage: "agent, decides",
  refund: "agent, pays out",
  notifier: "agent, emails",
  effects: "left the system",
};

// Region anchors spread around a sphere, like regions of a brain: sources on
// top, the agents around the middle, effects underneath.
const ANCHORS = [
  [0, 190, -20],
  [-170, 110, 40],
  [10, 80, -170],
  [-90, -10, 150],
  [170, -20, 50],
  [70, -130, -140],
  [-140, -150, -30],
].map(([x, y, z]) => new THREE.Vector3(x * 1.35, y * 1.35, z * 1.35));

const SIZE = [44, 32, 38, 24, 21, 21, 15]; // per lane
const C = {
  warm: "#D9D4C7",
  text: "#E5E5E5",
  white: "#FFFFFF",
  muted: "#B2B2B2",
  faint: "#6E6D75",
  dust: "#2B2A31",
  dim: "#26252B",
  gold: "#F5A623",
  thread: "#3A3940",
};
const HOP_MS = 140;

type N = {
  id: string;
  lane: number;
  x?: number;
  y?: number;
  z?: number;
  vx?: number;
  vy?: number;
  vz?: number;
};
type L = { id: string; source: string | N; target: string | N; type: string };

// Data flows one way along each edge type, whatever way the edge is stored.
function flow(e: GraphEdge): [string, string] | null {
  if (e.type === "INGESTED" || e.type === "READ") return [e.to, e.from];
  if (e.type === "WROTE" || e.type === "HANDED_OFF" || e.type === "CAUSED") return [e.from, e.to];
  return null;
}

// poisonedIds is the flagged page and the facts its research wrote.
function poisonedIds(s: State): Set<string> {
  const out = new Set<string>();
  if (s.blast.length === 0) return out;
  out.add(SOURCE);
  const readers = new Set(
    s.graph.edges.filter((e) => e.type === "INGESTED" && e.to === SOURCE).map((e) => e.from),
  );
  for (const e of s.graph.edges) if (e.type === "WROTE" && readers.has(e.from)) out.add(e.to);
  return out;
}

let glowTex: THREE.Texture | null = null;
function glow(): THREE.Texture {
  if (glowTex) return glowTex;
  const c = document.createElement("canvas");
  c.width = c.height = 64;
  const g = c.getContext("2d")!;
  const r = g.createRadialGradient(32, 32, 0, 32, 32, 32);
  r.addColorStop(0, "rgba(255,255,255,1)");
  r.addColorStop(0.12, "rgba(255,255,255,1)");
  r.addColorStop(0.22, "rgba(255,255,255,0.45)");
  r.addColorStop(0.5, "rgba(255,255,255,0.08)");
  r.addColorStop(1, "rgba(255,255,255,0)");
  g.fillStyle = r;
  g.fillRect(0, 0, 64, 64);
  glowTex = new THREE.CanvasTexture(c);
  return glowTex;
}

function tone(
  kind: string,
  st: UiStatus | undefined,
  props: Record<string, unknown>,
  dim: boolean,
): string {
  if (dim) return C.dim;
  if (kind === "Effect" && props.sent && !props.live) return C.faint;
  if (kind === "Fact" && props.active === false) return C.faint;
  switch (st) {
    case "poisoned":
      return C.white;
    case "rerun_changed":
    case "new":
    case "running":
      return C.gold;
    case "rerun_same":
      return C.muted;
    case "skipped":
    case "pending":
      return C.faint;
    case "superseded":
      return "#4A4950";
    default:
      return kind === "Effect" ? C.faint : C.warm;
  }
}

function Dust() {
  const geo = useMemo(() => {
    const n = 1800;
    const p = new Float32Array(n * 3);
    for (let i = 0; i < n; i++) {
      const r = 120 + Math.random() * 280;
      const t = Math.random() * Math.PI * 2;
      const u = Math.acos(2 * Math.random() - 1);
      p[i * 3] = r * Math.sin(u) * Math.cos(t);
      p[i * 3 + 1] = r * Math.cos(u) * 0.75;
      p[i * 3 + 2] = r * Math.sin(u) * Math.sin(t);
    }
    const g = new THREE.BufferGeometry();
    g.setAttribute("position", new THREE.BufferAttribute(p, 3));
    return g;
  }, []);
  return (
    <points geometry={geo}>
      <pointsMaterial
        color={C.faint}
        size={1.1}
        sizeAttenuation
        transparent
        opacity={0.45}
        depthWrite={false}
      />
    </points>
  );
}

type Sprite = { sprite: THREE.Sprite; base: number };

function Scene({
  s,
  onSelect,
  focusLane,
  autoFrame,
  labelEls,
}: {
  s: State;
  onSelect: (id: string | null) => void;
  focusLane: number | null;
  autoFrame: boolean;
  labelEls: React.RefObject<Map<string, HTMLDivElement>>;
}) {
  const fg = useRef<
    | {
        tickFrame: () => void;
        d3Force: (n: string, f?: unknown) => any;
        emitParticle: (l: L) => void;
      }
    | undefined
  >(undefined);
  const nodes = useRef(new Map<string, N>());
  const links = useRef(new Map<string, L>());
  const sprites = useRef(new Map<string, Sprite>());
  const flagged = s.blast.length > 0;

  // Keep node and link objects stable so the simulation keeps its positions;
  // new ones spawn near their lane's anchor.
  const ids = s.graph.nodes.map((n) => n.id).join("|") + "#" + s.graph.edges.length;
  const data = useMemo(() => {
    const keepN = new Map<string, N>();
    for (const n of s.graph.nodes) {
      const lane = LANE_OF(n);
      if (lane === null) continue;
      const old = nodes.current.get(n.id);
      if (old) keepN.set(n.id, old);
      else {
        const a = ANCHORS[lane];
        keepN.set(n.id, {
          id: n.id,
          lane,
          x: a.x + (Math.random() - 0.5) * 20,
          y: a.y + (Math.random() - 0.5) * 20,
          z: a.z + (Math.random() - 0.5) * 20,
        });
      }
    }
    const keepL = new Map<string, L>();
    for (const e of s.graph.edges) {
      const f = flow(e);
      if (!f || !keepN.has(f[0]) || !keepN.has(f[1])) continue;
      const id = `${e.from}|${e.type}|${e.to}`;
      keepL.set(id, links.current.get(id) ?? { id, source: f[0], target: f[1], type: e.type });
    }
    nodes.current = keepN;
    links.current = keepL;
    return { nodes: [...keepN.values()], links: [...keepL.values()] };
  }, [ids]); // eslint-disable-line react-hooks/exhaustive-deps

  // Each node pulls toward its lane's anchor, which is what makes the regions.
  useEffect(() => {
    const g = fg.current;
    if (!g) return;
    let list: N[] = [];
    const cluster = (alpha: number) => {
      for (const n of list) {
        const a = ANCHORS[n.lane];
        n.vx = (n.vx ?? 0) + (a.x - (n.x ?? 0)) * 0.1 * alpha;
        n.vy = (n.vy ?? 0) + (a.y - (n.y ?? 0)) * 0.1 * alpha;
        n.vz = (n.vz ?? 0) + (a.z - (n.z ?? 0)) * 0.1 * alpha;
      }
    };
    cluster.initialize = (ns: N[]) => (list = ns);
    g.d3Force("cluster", cluster);
    g.d3Force("charge")?.strength(-42).distanceMax(110);
    g.d3Force("link")?.distance(80).strength(0.002);
    g.d3Force("center", null);
    (g as unknown as { d3ReheatSimulation: () => void }).d3ReheatSimulation();
  }, []);

  const controls = useThree((st) => st.controls) as unknown as {
    target: THREE.Vector3;
    update: () => void;
  } | null;
  const camera = useThree((st) => st.camera) as THREE.PerspectiveCamera;
  const lo = useMemo(() => new THREE.Vector3(), []);
  const hi = useMemo(() => new THREE.Vector3(), []);
  const mid = useMemo(() => new THREE.Vector3(), []);
  const p = useMemo(() => new THREE.Vector3(), []);
  useFrame(() => {
    fg.current?.tickFrame();
    // While the view drifts on its own, keep the whole graph framed: the orbit
    // centre moves to the middle of its bounding box and the camera eases to
    // a distance that fits it. Once the viewer takes over, they frame it.
    if (!controls || !autoFrame || nodes.current.size === 0) return;
    lo.set(Infinity, Infinity, Infinity);
    hi.set(-Infinity, -Infinity, -Infinity);
    for (const n of nodes.current.values()) {
      p.set(n.x ?? 0, n.y ?? 0, n.z ?? 0);
      lo.min(p);
      hi.max(p);
    }
    mid.addVectors(lo, hi).multiplyScalar(0.5);
    const r = lo.distanceTo(hi) / 2 + 30;
    const want = (r / Math.sin((camera.fov * Math.PI) / 360)) * 0.6;
    controls.target.lerp(mid, 0.06);
    const off = camera.position.clone().sub(controls.target);
    off.setLength(off.length() + (want - off.length()) * 0.05);
    camera.position.copy(controls.target).add(off);
    controls.update();
  });

  // Labels are plain DOM over the canvas, moved to their projected point
  // every frame: regions beside their cluster's centre, nodes beside the node.
  const size = useThree((st) => st.size);
  const v = useMemo(() => new THREE.Vector3(), []);
  useFrame(() => {
    for (const [key, el] of labelEls.current) {
      if (key.startsWith("region:")) {
        const lane = Number(key.slice(7));
        let k = 0;
        v.set(0, 0, 0);
        for (const n of nodes.current.values()) {
          if (n.lane !== lane) continue;
          v.x += n.x ?? 0;
          v.y += n.y ?? 0;
          v.z += n.z ?? 0;
          k++;
        }
        if (!k) {
          el.style.display = "none";
          continue;
        }
        v.divideScalar(k).add(new THREE.Vector3(18, 16, 0));
      } else {
        const n = nodes.current.get(key.slice(5));
        if (!n) {
          el.style.display = "none";
          continue;
        }
        v.set((n.x ?? 0) + 7, (n.y ?? 0) - 2, n.z ?? 0);
      }
      v.project(camera);
      if (v.z > 1) {
        el.style.display = "none";
        continue;
      }
      el.style.display = "";
      el.style.transform = `translate(${((v.x + 1) / 2) * size.width}px, ${((1 - v.y) / 2) * size.height}px)`;
    }
  });

  // Hop distance from the flagged page, for the pulse.
  const hops = useMemo(() => {
    const out = new Map<string, number>();
    if (!flagged) return out;
    const next = new Map<string, string[]>();
    for (const e of s.graph.edges) {
      const f = flow(e);
      if (f) next.set(f[0], [...(next.get(f[0]) ?? []), f[1]]);
    }
    out.set(SOURCE, 0);
    const q = [SOURCE];
    while (q.length) {
      const id = q.shift()!;
      for (const n of next.get(id) ?? [])
        if (!out.has(n)) (out.set(n, out.get(id)! + 1), q.push(n));
    }
    return out;
  }, [flagged, s.graph.edges]);
  const flagAt = useRef<number | null>(null);
  useEffect(() => {
    flagAt.current = flagged ? performance.now() : null;
  }, [flagged]);

  // Decisions pop the node once.
  const pops = useRef(new Map<string, number>());
  const last = useRef(new Map<string, string | undefined>());
  useEffect(() => {
    const now = performance.now();
    for (const n of s.graph.nodes) {
      const st = statusOf(s, n.id);
      const prev = last.current.get(n.id);
      last.current.set(n.id, st);
      if (prev !== undefined && prev !== st && st !== "pending") pops.current.set(n.id, now);
    }
  });

  const focus = useMemo(() => {
    const f = new Set(s.blast);
    for (const n of s.graph.nodes) if (statusOf(s, n.id) === "new") f.add(n.id);
    for (const e of s.graph.edges)
      if (f.has(e.from) && ["WROTE", "INGESTED", "CAUSED"].includes(e.type)) f.add(e.to);
    return f;
  }, [s]);
  const readers = new Set(
    s.graph.edges.filter((e) => e.type === "INGESTED" && e.to === SOURCE).map((e) => e.from),
  );
  const byId = useMemo(() => new Map(s.graph.nodes.map((n) => [n.id, n])), [s.graph.nodes]);

  const statusFor = (id: string): UiStatus | undefined => {
    const n = byId.get(id);
    if (!n) return undefined;
    let st = statusOf(s, id);
    if (
      flagged &&
      (id === SOURCE ||
        (n.label === "Fact" &&
          s.graph.edges.some((e) => e.type === "WROTE" && e.to === id && readers.has(e.from))))
    )
      st = "poisoned";
    return st === "ok" ? undefined : st;
  };

  // Colours follow state without rebuilding the objects.
  useEffect(() => {
    for (const [id, sp] of sprites.current) {
      const n = byId.get(id);
      if (!n) continue;
      const nd = nodes.current.get(id);
      const lane = nd?.lane ?? 3;
      const dim = (flagged && !focus.has(id)) || (focusLane !== null && lane !== focusLane);
      (sp.sprite.material as THREE.SpriteMaterial).color.set(
        tone(n.label, statusFor(id), n.props, dim),
      );
    }
  });

  // Pulse, pops and the running node breathe every frame.
  useFrame(() => {
    const now = performance.now();
    for (const [id, sp] of sprites.current) {
      let k = 1;
      const h = hops.get(id);
      if (flagAt.current !== null && h !== undefined) {
        const t = now - (flagAt.current + h * HOP_MS);
        if (t > 0 && t < 500) k += 1.4 * (1 - t / 500);
      }
      const p = pops.current.get(id);
      if (p !== undefined && now - p < 450) k += 0.9 * Math.sin(((now - p) / 450) * Math.PI);
      if (statusOf(s, id) === "running") k += 0.35 * (1 + Math.sin(now / 160));
      if (id === s.selected) k *= 1.5;
      sp.sprite.scale.setScalar(sp.base * k);
    }
  });

  const nodeObject = useCallback((n: N) => {
    const mat = new THREE.SpriteMaterial({
      map: glow(),
      color: C.warm,
      transparent: true,
      depthWrite: false,
      toneMapped: false,
    });
    const sprite = new THREE.Sprite(mat);
    const base = SIZE[n.lane] * (n.id === SOURCE ? 1.4 : 1);
    sprite.scale.setScalar(base);
    sprites.current.set(n.id, { sprite, base });
    return sprite;
  }, []);

  const path = new Set((s.path?.edges ?? []).map((e) => `${e.from}|${e.type}|${e.to}`));
  const repaired = (l: L) => {
    const t = typeof l.target === "string" ? l.target : l.target.id;
    return ["rerun_changed", "new"].includes(statusOf(s, t) ?? "");
  };
  const inBlast = (l: L) => {
    const a = typeof l.source === "string" ? l.source : l.source.id;
    const b = typeof l.target === "string" ? l.target : l.target.id;
    return focus.has(a) && focus.has(b);
  };

  return (
    <>
      <ForceGraph
        ref={fg as never}
        graphData={data}
        nodeThreeObject={nodeObject as never}
        linkColor={(l: L) =>
          path.has(l.id)
            ? C.white
            : repaired(l)
              ? C.gold
              : flagged && !inBlast(l)
                ? C.dust
                : C.thread
        }
        linkOpacity={0.55}
        linkWidth={(l: L) => (path.has(l.id) ? 0.9 : 0)}
        linkDirectionalParticles={(l: L) =>
          path.has(l.id)
            ? 4
            : repaired(l)
              ? 2
              : flagged && inBlast(l) && s.phase === "flagged"
                ? 1
                : 0
        }
        linkDirectionalParticleWidth={(l: L) => (path.has(l.id) ? 1.6 : 1.1)}
        linkDirectionalParticleColor={(l: L) => (repaired(l) && !path.has(l.id) ? C.gold : C.white)}
        linkDirectionalParticleSpeed={0.006}
        warmupTicks={40}
        cooldownTicks={Infinity}
        d3VelocityDecay={0.32}
        onNodeClick={(n: N) => {
          const g = byId.get(n.id);
          if (g?.label === "Action") onSelect(n.id);
          else if (g?.label === "Effect") {
            const by = s.graph.edges.find((e) => e.type === "CAUSED" && e.to === n.id)?.from;
            if (by) onSelect(by);
          }
        }}
      />
    </>
  );
}

export function Atlas3D({
  s,
  onSelect,
  inset = 0,
}: {
  s: State;
  onSelect: (id: string | null) => void;
  // Width covered on the right by the open drawer; the scene stays left of it.
  inset?: number;
}) {
  const [spin, setSpin] = useState(true);
  const [focusLane, setFocusLane] = useState<number | null>(null);
  const labelEls = useRef(new Map<string, HTMLDivElement>());
  const keep = (key: string) => (el: HTMLDivElement | null) => {
    if (el) labelEls.current.set(key, el);
    else labelEls.current.delete(key);
  };
  const poisoned = poisonedIds(s);
  const named = s.graph.nodes.filter((n) => n.id === s.selected || poisoned.has(n.id));
  const counts = LANES.map((_, i) => s.graph.nodes.filter((n) => LANE_OF(n) === i).length);
  return (
    <div
      className="absolute inset-y-0 left-0 bg-bg transition-[right] duration-200"
      style={{ right: inset }}
    >
      <Canvas
        camera={{ fov: 45, near: 1, far: 4000, position: [0, 70, 640] }}
        dpr={[1, 2]}
        onPointerMissed={() => onSelect(null)}
      >
        <color attach="background" args={["#0B0A0E"]} />
        <Dust />
        <Scene
          s={s}
          onSelect={onSelect}
          focusLane={focusLane}
          autoFrame={spin}
          labelEls={labelEls}
        />
        <OrbitControls
          makeDefault
          enableDamping
          dampingFactor={0.08}
          autoRotate={spin}
          autoRotateSpeed={0.35}
          onStart={() => setSpin(false)}
        />
        <EffectComposer>
          <Bloom
            mipmapBlur
            intensity={2.2}
            luminanceThreshold={0.08}
            luminanceSmoothing={0.4}
            radius={0.8}
          />
        </EffectComposer>
        <GizmoHelper alignment="bottom-right" margin={[56, 56]}>
          <GizmoViewport
            axisColors={[C.faint, C.faint, C.faint]}
            labelColor={C.text}
            hideNegativeAxes
          />
        </GizmoHelper>
      </Canvas>
      <div className="pointer-events-none absolute inset-0 overflow-hidden">
        {LANES.map((name, i) => (
          <div
            key={name}
            ref={keep(`region:${i}`)}
            className="absolute top-0 left-0 whitespace-nowrap"
            style={{ display: "none" }}
          >
            <div className="font-mono text-[11px] leading-[14px] text-text">{name}</div>
            <div className="font-mono text-[10px] leading-[13px] text-faint">
              {REGION[name]} {counts[i]}
            </div>
          </div>
        ))}
        {named.map((n) => {
          const st = statusOf(s, n.id);
          return (
            <div
              key={n.id}
              ref={keep(`node:${n.id}`)}
              className="absolute top-0 left-0 whitespace-nowrap"
              style={{ display: "none" }}
            >
              <span
                className={cn(
                  "rounded-sm bg-bg/70 px-1 font-mono text-[10.5px]",
                  st === "rerun_changed" || st === "new" ? "text-gold" : "text-muted",
                )}
              >
                {labelFor(n, s.graph, st).replaceAll(String.fromCharCode(0x336), "")}
              </span>
            </div>
          );
        })}
      </div>
      <div className="pointer-events-auto absolute top-3 left-3 w-[220px] rounded-md border border-rule bg-bg/80 p-2.5">
        <div className="mb-1.5 font-mono text-[11px] text-faint">{LANES.length} regions</div>
        {LANES.map((name, i) => (
          <button
            key={name}
            type="button"
            onMouseEnter={() => setFocusLane(i)}
            onMouseLeave={() => setFocusLane(null)}
            className="flex w-full items-center gap-2 rounded-sm px-1 py-[3px] text-left hover:bg-raised"
          >
            <span
              className="size-2 rounded-[2px] bg-[#D9D4C7]"
              style={{ opacity: 0.35 + 0.65 * (1 - i / LANES.length) }}
            />
            <span className="flex-1">
              <span className="block font-mono text-[11.5px] leading-[14px] text-text">{name}</span>
              <span className="block font-mono text-[10px] leading-[13px] text-faint">
                {REGION[name]}
              </span>
            </span>
            <span className="font-mono text-[11px] text-muted">{counts[i]}</span>
          </button>
        ))}
      </div>
      <div className="pointer-events-none absolute top-3 right-3 font-mono text-[11px] text-faint">
        drag to rotate, scroll to zoom
      </div>
    </div>
  );
}
