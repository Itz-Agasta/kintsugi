import { Suspense, useEffect, useMemo, useRef } from "react";
import { Canvas, useFrame, useThree } from "@react-three/fiber";
import { Line, OrbitControls, RoundedBox, Text } from "@react-three/drei";
import * as THREE from "three";
import monoFont from "@fontsource/fragment-mono/files/fragment-mono-latin-400-normal.woff?url";
import { LANES, LANE_H, layout } from "@/dashboard/layout";
import { labelFor, pillWidth } from "@/dashboard/marks";
import { statusOf, type State, type UiStatus } from "@/dashboard/state";
import { SOURCE, type GraphEdge } from "@/lib/types";

// The provenance graph as a stack of lanes in depth: same x as the flat view,
// each lane one step down and one step back. Unlit materials keep the palette.

const C = {
  bg: "#0F0E12",
  panel: "#18171C",
  raised: "#222127",
  line: "#33323A",
  rule: "#25242A",
  text: "#E5E5E5",
  muted: "#B2B2B2",
  faint: "#6E6D75",
  gold: "#F5A623",
  goldInk: "#1A1205",
  goldDim: "#3A2A10",
};

const U = 1 / 100; // layout units to scene units
const DY = 0.66; // lane step down
const DZ = 0.95; // lane step back
const PILL_H = 0.25;
const STROKE = String.fromCharCode(0x336);
const HOP_MS = 170; // blast pulse: time per hop

type Look = { fill: string; edge: string; text: string; opacity: number; struck?: boolean };

function look(kind: string, st: UiStatus | undefined, props: Record<string, unknown>): Look {
  const undone = kind === "Effect" && props.sent && !props.live;
  if (kind === "Fact" && props.active === false)
    return { fill: C.panel, edge: C.faint, text: C.faint, opacity: 1, struck: true };
  if (undone) return { fill: C.panel, edge: C.faint, text: C.faint, opacity: 1, struck: true };
  switch (st) {
    case "poisoned":
      return { fill: C.text, edge: C.text, text: C.panel, opacity: 1 };
    case "rerun_changed":
      return { fill: C.gold, edge: C.gold, text: C.goldInk, opacity: 1 };
    case "rerun_same":
      return { fill: C.raised, edge: C.muted, text: C.text, opacity: 1 };
    case "skipped":
      return { fill: C.panel, edge: C.line, text: C.muted, opacity: 1 };
    case "superseded":
      return { fill: C.panel, edge: C.faint, text: C.faint, opacity: 1, struck: true };
    case "new":
      return { fill: C.panel, edge: C.gold, text: C.gold, opacity: 1 };
    case "running":
      return { fill: C.goldDim, edge: C.gold, text: C.gold, opacity: 1 };
    case "pending":
      return { fill: C.panel, edge: C.faint, text: C.faint, opacity: 1 };
    default:
      return {
        fill: C.panel,
        edge: C.line,
        text: kind === "Action" ? C.muted : C.text,
        opacity: 1,
      };
  }
}

// Data flows one way along each edge type, whatever way the edge is stored.
function flow(e: GraphEdge): [string, string] | null {
  switch (e.type) {
    case "INGESTED":
    case "READ":
      return [e.to, e.from];
    case "WROTE":
    case "HANDED_OFF":
    case "CAUSED":
      return [e.from, e.to];
    default:
      return null;
  }
}

type Vec = [number, number, number];
type Particle = { from: Vec; to: Vec; t0: number; dur: number; color: string };

function Pill({
  id,
  at,
  label,
  lk,
  dim,
  selected,
  litAt,
  popAt,
  onSelect,
}: {
  id: string;
  at: Vec;
  label: string;
  lk: Look;
  dim: boolean;
  selected: boolean;
  litAt: number | null;
  popAt: number | null;
  onSelect: (id: string) => void;
}) {
  const group = useRef<THREE.Group>(null);
  const fill = useRef<THREE.MeshBasicMaterial>(null);
  const w = pillWidth(label) * U;
  const base = useMemo(() => new THREE.Color(lk.fill), [lk.fill]);
  const flash = useMemo(() => new THREE.Color(C.text), []);
  // Position eases to its target, the blast pulse flashes it once, and a
  // repair decision pops it.
  useFrame(() => {
    const g = group.current;
    if (!g) return;
    g.position.lerp(new THREE.Vector3(...at), 0.18);
    const now = performance.now();
    let k = 0;
    if (litAt !== null && now >= litAt) k = Math.max(0, 1 - (now - litAt) / 450);
    if (fill.current) fill.current.color.copy(base).lerp(flash, k * 0.85);
    const p = popAt !== null ? Math.max(0, 1 - (now - popAt) / 380) : 0;
    g.scale.setScalar(1 + 0.22 * Math.sin(p * Math.PI));
    const o = dim && (litAt === null || now < litAt) ? 0.22 : lk.opacity;
    g.traverse((c) => {
      const m = (c as THREE.Mesh).material as THREE.Material & { opacity?: number };
      if (m && "opacity" in m) {
        m.transparent = true;
        m.opacity = o;
      }
    });
  });
  const text = label.replaceAll(STROKE, "");
  return (
    <group
      ref={group}
      position={at}
      onClick={(e) => {
        e.stopPropagation();
        onSelect(id);
      }}
    >
      {selected && (
        <RoundedBox args={[w + 0.1, PILL_H + 0.1, 0.02]} radius={0.06} position={[0, 0, -0.02]}>
          <meshBasicMaterial color={C.text} />
        </RoundedBox>
      )}
      <RoundedBox args={[w + 0.02, PILL_H + 0.02, 0.03]} radius={0.05} position={[0, 0, -0.005]}>
        <meshBasicMaterial color={lk.edge} />
      </RoundedBox>
      <RoundedBox args={[w, PILL_H, 0.04]} radius={0.045}>
        <meshBasicMaterial ref={fill} color={lk.fill} />
      </RoundedBox>
      <Text font={monoFont} fontSize={0.115} color={lk.text} position={[0, 0, 0.03]}>
        {text}
      </Text>
      {lk.struck && (
        <mesh position={[0, 0, 0.031]}>
          <planeGeometry args={[w * 0.62, 0.012]} />
          <meshBasicMaterial color={lk.text} />
        </mesh>
      )}
    </group>
  );
}

function Particles({ list }: { list: React.RefObject<Particle[]> }) {
  const MAX = 160;
  const mesh = useRef<THREE.InstancedMesh>(null);
  const tmp = useMemo(() => new THREE.Object3D(), []);
  const col = useMemo(() => new THREE.Color(), []);
  useFrame(() => {
    const m = mesh.current;
    if (!m) return;
    const now = performance.now();
    list.current = list.current.filter((p) => now - p.t0 < p.dur);
    let i = 0;
    for (const p of list.current) {
      if (i >= MAX) break;
      const t = Math.max(0, (now - p.t0) / p.dur);
      if (t <= 0) continue;
      tmp.position.set(
        p.from[0] + (p.to[0] - p.from[0]) * t,
        p.from[1] + (p.to[1] - p.from[1]) * t,
        p.from[2] + (p.to[2] - p.from[2]) * t + 0.05,
      );
      tmp.scale.setScalar(1);
      tmp.updateMatrix();
      m.setMatrixAt(i, tmp.matrix);
      m.setColorAt(i, col.set(p.color));
      i++;
    }
    m.count = i;
    m.instanceMatrix.needsUpdate = true;
    if (m.instanceColor) m.instanceColor.needsUpdate = true;
  });
  return (
    <instancedMesh ref={mesh} args={[undefined, undefined, MAX]}>
      <sphereGeometry args={[0.035, 10, 10]} />
      <meshBasicMaterial />
    </instancedMesh>
  );
}

function Front({ x, height, depth }: { x: number | null; height: number; depth: number }) {
  const mesh = useRef<THREE.Mesh>(null);
  useFrame(() => {
    const m = mesh.current;
    if (!m) return;
    m.visible = x !== null;
    if (x !== null) m.position.x += (x - m.position.x) * 0.15;
  });
  return (
    <mesh
      ref={mesh}
      position={[x ?? 0, -height / 2 + DY / 2, -depth / 2 + DZ / 2]}
      rotation={[Math.atan2(DZ, DY), 0, 0]}
    >
      <planeGeometry args={[0.02, Math.hypot(height, depth) + 0.6]} />
      <meshBasicMaterial color={C.gold} transparent opacity={0.9} />
      <mesh>
        <planeGeometry args={[0.5, Math.hypot(height, depth) + 0.6]} />
        <meshBasicMaterial color={C.gold} transparent opacity={0.06} depthWrite={false} />
      </mesh>
    </mesh>
  );
}

// Rig frames the stack frontally on first show, then eases into a
// three-quarter tilt. When the stack or the canvas changes size it eases from
// wherever the camera is to the new framing; in between, the viewer orbits.
function Rig({ center, span }: { center: Vec; span: number }) {
  const { camera, size } = useThree();
  const controls = useThree((st) => st.controls) as unknown as {
    target: THREE.Vector3;
    update: () => void;
  } | null;
  const fov = ((camera as THREE.PerspectiveCamera).fov * Math.PI) / 180;
  const aspect = size.width / Math.max(size.height, 1);
  // Far enough to fit the stack across and top to bottom, with a margin.
  const d = (Math.max(span / aspect, LANES.length * DY) / (2 * Math.tan(fov / 2))) * 1.08 + 0.6;
  const c = new THREE.Vector3(...center);
  // The tilted view fits the stack's bounding sphere, which is larger than
  // its frontal face.
  const r = 0.5 * Math.hypot(span, LANES.length * DY, LANES.length * DZ);
  const dTilt = (r / Math.sin(Math.min(fov, 2 * Math.atan(Math.tan(fov / 2) * aspect)) / 2)) * 0.42;
  const tilt = new THREE.Vector3(-0.62, 0.22, 0.75).normalize().multiplyScalar(dTilt).add(c);
  const anim = useRef<{ t0: number; from: THREE.Vector3 } | null>(null);
  const first = useRef(true);
  const key = `${span.toFixed(2)}|${size.width}|${size.height}`;
  useEffect(() => {
    const from = first.current ? new THREE.Vector3(c.x, c.y, c.z + d) : camera.position.clone();
    first.current = false;
    anim.current = { t0: performance.now(), from };
  }, [key]); // eslint-disable-line react-hooks/exhaustive-deps
  useFrame(() => {
    const a = anim.current;
    if (!a) return;
    const t = Math.min(1, (performance.now() - a.t0) / 1400);
    const e = 1 - Math.pow(1 - t, 3);
    camera.position.copy(a.from.clone().lerp(tilt, e));
    if (controls) {
      controls.target.copy(c);
      controls.update();
    } else camera.lookAt(c);
    if (t >= 1) anim.current = null;
  });
  return null;
}

export function Stack3D({
  s,
  onSelect,
  inset = 0,
}: {
  s: State;
  onSelect: (id: string | null) => void;
  // Width covered on the right by the open drawer; the scene stays left of it.
  inset?: number;
}) {
  const flagged = s.blast.length > 0;
  const { pos, kept } = useMemo(
    () => layout(s.graph, flagged ? s.blast : null),
    [s.graph, s.blast, flagged],
  );
  const at = (id: string): Vec | null => {
    const p = pos.get(id);
    if (!p) return null;
    const lane = Math.round(p.y / LANE_H);
    return [p.x * U, -lane * DY - (p.y - lane * LANE_H) * U, -lane * DZ];
  };
  const xs = [...pos.values()].map((p) => p.x * U);
  const x0 = xs.length ? Math.min(...xs) - 0.9 : 0;
  const x1 = Math.max(
    xs.length ? Math.max(...xs) + 0.9 : 1,
    kept && kept.count ? kept.x * U + 0.7 : 0,
  );
  const span = x1 - x0;
  const height = LANES.length * DY;
  const depth = LANES.length * DZ;
  const center: Vec = [(x0 + x1) / 2, -height / 2 + DY / 2, -depth / 2 + DZ / 2];

  // Hop distance from the flagged page, walking data flow forward.
  const hops = useMemo(() => {
    const out = new Map<string, number>();
    if (!flagged) return out;
    const next = new Map<string, string[]>();
    for (const e of s.graph.edges) {
      const f = flow(e);
      if (f) next.set(f[0], [...(next.get(f[0]) ?? []), f[1]]);
    }
    out.set(SOURCE, 0);
    const queue = [SOURCE];
    while (queue.length) {
      const id = queue.shift()!;
      for (const n of next.get(id) ?? []) {
        if (!out.has(n)) {
          out.set(n, out.get(id)! + 1);
          queue.push(n);
        }
      }
    }
    return out;
  }, [flagged, s.graph.edges]);

  // The pulse starts when the flag lands.
  const flagAt = useRef<number | null>(null);
  useEffect(() => {
    flagAt.current = flagged ? performance.now() : null;
  }, [flagged]);

  const particles = useRef<Particle[]>([]);
  const seenEdges = useRef(new Set<string>());
  const pops = useRef(new Map<string, number>());
  const lastStatus = useRef(new Map<string, string | undefined>());

  // Emit particles for edges as they appear, and gold ones for decisions.
  useEffect(() => {
    const now = performance.now();
    for (const e of s.graph.edges) {
      const id = `${e.from}|${e.type}|${e.to}`;
      const f = flow(e);
      if (!f || seenEdges.current.has(id)) continue;
      seenEdges.current.add(id);
      const a = at(f[0]);
      const b = at(f[1]);
      if (a && b) particles.current.push({ from: a, to: b, t0: now, dur: 650, color: C.muted });
    }
    for (const n of s.graph.nodes) {
      if (n.label !== "Action") continue;
      const st = statusOf(s, n.id);
      const prev = lastStatus.current.get(n.id);
      lastStatus.current.set(n.id, st);
      if (prev === undefined || prev === st || st === "pending" || st === "running") continue;
      pops.current.set(n.id, now);
      if (st === "rerun_changed" || st === "new") {
        for (const e of s.graph.edges) {
          const f = flow(e);
          if (!f || f[1] !== n.id) continue;
          const a = at(f[0]);
          const b = at(f[1]);
          if (a && b) particles.current.push({ from: a, to: b, t0: now, dur: 520, color: C.gold });
        }
      }
    }
  });

  // On flag, a white pulse walks the blast radius hop by hop.
  useEffect(() => {
    if (!flagged) return;
    const start = performance.now();
    for (const e of s.graph.edges) {
      const f = flow(e);
      if (!f) continue;
      const h = hops.get(f[0]);
      const a = at(f[0]);
      const b = at(f[1]);
      if (h === undefined || !a || !b || !hops.has(f[1])) continue;
      particles.current.push({
        from: a,
        to: b,
        t0: start + h * HOP_MS,
        dur: HOP_MS * 1.6,
        color: C.text,
      });
    }
  }, [flagged]); // eslint-disable-line react-hooks/exhaustive-deps

  const focus = new Set(s.blast);
  for (const n of s.graph.nodes) if (statusOf(s, n.id) === "new") focus.add(n.id);
  for (const e of s.graph.edges) {
    if (focus.has(e.from) && ["WROTE", "INGESTED", "CAUSED"].includes(e.type)) focus.add(e.to);
  }
  const readers = new Set(
    s.graph.edges.filter((e) => e.type === "INGESTED" && e.to === SOURCE).map((e) => e.from),
  );
  const pathEdges = new Set((s.path?.edges ?? []).map((e) => `${e.from}|${e.type}|${e.to}`));

  const frontX = (() => {
    if (s.front === null) return null;
    const a = s.graph.nodes.find((n) => n.label === "Action" && Number(n.props.seq) === s.front);
    const p = a && pos.get(a.id);
    return p ? p.x * U + 0.55 : null;
  })();

  return (
    <div
      className="absolute inset-y-0 left-0 bg-canvas transition-[right] duration-200"
      style={{ right: inset }}
    >
      <Canvas
        camera={{ fov: 32, near: 0.1, far: 400, position: [center[0], center[1], 30] }}
        dpr={[1, 2]}
        onPointerMissed={() => onSelect(null)}
      >
        <color attach="background" args={[C.bg]} />
        <OrbitControls makeDefault enableDamping dampingFactor={0.08} />
        <Rig center={center} span={span} />
        <Suspense fallback={null}>
          {LANES.map((name, i) => (
            <group key={name} position={[0, -i * DY, -i * DZ]}>
              <mesh position={[(x0 + x1) / 2, 0, -0.05]}>
                <planeGeometry args={[span, DY * 0.86]} />
                <meshBasicMaterial
                  color={i % 2 ? C.panel : C.rule}
                  transparent
                  opacity={0.55}
                  depthWrite={false}
                />
              </mesh>
              <Line
                points={[
                  [x0, DY * 0.43, -0.04],
                  [x1, DY * 0.43, -0.04],
                ]}
                color={C.line}
                lineWidth={1}
              />
              <Text
                font={monoFont}
                fontSize={0.14}
                color={C.faint}
                anchorX="left"
                position={[x0 + 0.15, DY * 0.3, 0]}
              >
                {name}
              </Text>
            </group>
          ))}
          {s.graph.edges.map((e) => {
            const f = flow(e);
            const a = at(e.from);
            const b = at(e.to);
            if (!f || !a || !b) return null;
            const id = `${e.from}|${e.type}|${e.to}`;
            const down =
              e.type === "HANDED_OFF"
                ? e.to
                : e.type === "READ" || e.type === "CAUSED"
                  ? e.from
                  : null;
            const repaired = down && ["rerun_changed", "new"].includes(statusOf(s, down) ?? "");
            const dim = flagged && (!focus.has(e.from) || !focus.has(e.to));
            const seam = pathEdges.has(id);
            const faintType = ["INGESTED", "READ", "WROTE", "CAUSED"].includes(e.type);
            return (
              <Line
                key={id}
                points={[a, b]}
                color={seam || repaired ? C.gold : faintType ? C.line : C.faint}
                lineWidth={seam ? 3 : repaired ? 1.6 : 1}
                transparent
                opacity={dim ? 0.15 : seam ? 1 : repaired ? 0.8 : 0.9}
              />
            );
          })}
          {s.graph.nodes.map((n) => {
            const p = at(n.id);
            if (!p || n.label === "Agent") return null;
            let st = statusOf(s, n.id);
            if (
              (n.label === "Source" && flagged && n.id === SOURCE) ||
              (n.label === "Fact" &&
                flagged &&
                s.graph.edges.some(
                  (e) => e.type === "WROTE" && e.to === n.id && readers.has(e.from),
                ))
            )
              st = "poisoned";
            if (st === "ok") st = undefined;
            const h = hops.get(n.id);
            return (
              <Pill
                key={n.id}
                id={n.id}
                at={p}
                label={labelFor(n, s.graph, st)}
                lk={look(n.label, st, n.props)}
                dim={flagged && !focus.has(n.id)}
                selected={n.id === s.selected}
                litAt={
                  flagAt.current !== null && h !== undefined ? flagAt.current + h * HOP_MS : null
                }
                popAt={pops.current.get(n.id) ?? null}
                onSelect={(id) => n.label === "Action" && onSelect(id)}
              />
            );
          })}
          {kept && kept.count > 0 && (
            <Text
              font={monoFont}
              fontSize={0.14}
              color={C.faint}
              textAlign="center"
              position={[kept.x * U, -3 * DY, -3 * DZ]}
            >
              {`${kept.tickets[0]} to ${kept.tickets.at(-1)}\n${kept.count} tickets kept`}
            </Text>
          )}
        </Suspense>
        <Particles list={particles} />
        <Front x={frontX} height={height} depth={depth} />
      </Canvas>
    </div>
  );
}
