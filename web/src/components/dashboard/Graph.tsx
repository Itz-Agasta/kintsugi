import { useEffect, useMemo, useRef, useState } from "react";
import cytoscape, { type Core, type ElementDefinition } from "cytoscape";
import { ArrowRightIcon } from "@phosphor-icons/react";
import { COL_W, LANE_H, LANES, layout } from "@/dashboard/layout";
import { ICONS, classesFor, labelFor, pillWidth, stylesheet } from "@/dashboard/marks";
import { statusOf, type State, type UiStatus } from "@/dashboard/state";
import { SOURCE, type GraphEdge } from "@/lib/types";

const DRAWN = new Set(["INGESTED", "WROTE", "READ", "HANDED_OFF", "CAUSED"]);
const REPAIRED: UiStatus[] = ["rerun_changed", "new"];
const LANE_ICON = ["source", "research", "fact", "triage", "refund", "notifier", "effect"];

type View = { zoom: number; x: number; y: number };

// seam returns the edge ids on the gold causal path. The path API walks FEEDS
// edges, which the canvas draws as the WROTE and READ pair through a fact.
function seam(s: State): { edges: Set<string>; nodes: Set<string> } {
  const edges = new Set<string>();
  const nodes = new Set<string>(s.path?.nodes ?? []);
  const id = (e: GraphEdge) => `${e.from}|${e.type}|${e.to}`;
  for (const e of s.path?.edges ?? []) {
    if (e.type !== "FEEDS") {
      edges.add(id(e));
      continue;
    }
    for (const w of s.graph.edges) {
      if (w.type !== "WROTE" || w.from !== e.from) continue;
      const r = s.graph.edges.find((x) => x.type === "READ" && x.from === e.to && x.to === w.to);
      if (r) {
        edges.add(id(w));
        edges.add(id(r));
        nodes.add(w.to);
      }
    }
  }
  // On from the selected action to what it handed off to, now.
  if (s.path && s.selected) {
    for (const e of s.graph.edges) {
      if (e.type === "HANDED_OFF" && e.from === s.selected && statusOf(s, e.to) !== "superseded") {
        edges.add(id(e));
        nodes.add(e.to);
      }
    }
  }
  return { edges, nodes };
}

// stretch moves a node's lane by k but keeps its offset inside the lane, so
// stacked effects keep their spacing.
function stretch(y: number, k: number): number {
  const lane = Math.round(y / LANE_H) * LANE_H;
  return lane * k + (y - lane);
}

const GUTTER = 120;
const STACK_ROOM = 64; // layout units below the effects lane for stacked effects // room for the lane labels on the left
const PILL = 120; // widest pill, so edge columns are not cut off

// frame picks a zoom that fits every column (never below 0.75, so pills stay
// readable; pan for the rest) and a lane stretch k that fills the height.
function frame(
  xs: number[],
  w: number,
  h: number,
): { zoom: number; k: number; pan: { x: number; y: number } } {
  const x1 = Math.min(...xs) - PILL / 2;
  const span = Math.max(...xs) + PILL / 2 - x1;
  const zoom = Math.max(0.75, Math.min((w - GUTTER - 24) / span, 1.2));
  // Effects stack below their lane's centre (up to four for one ticket), and
  // that stack is not stretched, so the height budget keeps room for it.
  const k = Math.max(0.5, Math.min((h - STACK_ROOM * zoom) / (zoom * LANES.length * LANE_H), 2.5));
  return { zoom, k, pan: { x: GUTTER - x1 * zoom, y: (LANE_H * k * zoom) / 2 } };
}

export function Graph({
  s,
  onSelect,
  inset = 0,
}: {
  s: State;
  onSelect: (id: string | null) => void;
  // Width covered on the right by the open drawer, kept clear when framing.
  inset?: number;
}) {
  const box = useRef<HTMLDivElement>(null);
  const cyRef = useRef<Core | null>(null);
  const [ready, setReady] = useState(false);
  const [view, setView] = useState<View>({ zoom: 1, x: 0, y: 0 });
  const [size, setSize] = useState({ w: 0, h: 0 });
  const select = useRef(onSelect);
  useEffect(() => {
    select.current = onSelect;
  }, [onSelect]);
  const fitKey = useRef("");

  const flagged = s.blast.length > 0;
  const { pos, kept } = useMemo(
    () => layout(s.graph, flagged ? s.blast : null),
    [s.graph, s.blast, flagged],
  );
  const framing = useMemo(
    () =>
      pos.size && size.h
        ? frame(
            [...[...pos.values()].map((p) => p.x), ...(kept?.count ? [kept.x] : [])],
            size.w - inset,
            size.h,
          )
        : null,
    [pos, kept, size, inset],
  );
  const k = framing?.k ?? 1;

  useEffect(() => {
    let cy: Core | null = null;
    let raf = 0;
    let gone = false;
    let ro: ResizeObserver | null = null;
    void document.fonts.ready.then(() => {
      if (gone || !box.current) return;
      cy = cytoscape({
        container: box.current,
        style: stylesheet(),
        layout: { name: "preset" },
        minZoom: 0.25,
        maxZoom: 2.5,
        boxSelectionEnabled: false,
        autoungrabify: true,
      });
      const sync = () => {
        cancelAnimationFrame(raf);
        raf = requestAnimationFrame(() => {
          if (!cy) return;
          const p = cy.pan();
          setView({ zoom: cy.zoom(), x: p.x, y: p.y });
        });
      };
      cy.on("viewport resize", sync);
      cy.on("tap", (e) => {
        if (e.target === cy) return select.current(null);
        const t = e.target as cytoscape.SingularElementArgument;
        if (t.isNode() && t.hasClass("kind-action")) select.current(t.id());
        else if (t.isNode() && t.hasClass("kind-effect")) {
          const by = t.incomers("node.kind-action").first();
          if (by.nonempty()) select.current(by.id());
        }
      });
      // Cytoscape only watches the window; the panel can resize on its own.
      ro = new ResizeObserver(() => {
        cy?.resize();
        if (cy) setSize({ w: cy.width(), h: cy.height() });
      });
      ro.observe(box.current);
      cyRef.current = cy;
      setReady(true);
      setSize({ w: cy.width(), h: cy.height() });
      sync();
    });
    return () => {
      gone = true;
      cancelAnimationFrame(raf);
      ro?.disconnect();
      cy?.destroy();
      cyRef.current = null;
    };
  }, []);

  useEffect(() => {
    const cy = cyRef.current;
    if (!cy || !ready) return;
    const focus = new Set(s.blast);
    for (const n of s.graph.nodes) if (statusOf(s, n.id) === "new") focus.add(n.id);
    // Facts, sources and effects belong to the focus through their action.
    for (const e of s.graph.edges) {
      if (focus.has(e.from) && ["WROTE", "INGESTED", "CAUSED"].includes(e.type)) focus.add(e.to);
    }
    const dimmed = (id: string) => flagged && !focus.has(id);
    // Facts written by the research that read the flagged page carry its poison.
    const readers = new Set(
      s.graph.edges.filter((e) => e.type === "INGESTED" && e.to === SOURCE).map((e) => e.from),
    );
    const poisonedBy = new Set(
      s.graph.edges
        .filter((e) => flagged && e.type === "WROTE" && readers.has(e.from))
        .map((e) => e.to),
    );
    const newBy = new Set(
      s.graph.edges
        .filter((e) => e.type === "CAUSED" && statusOf(s, e.from) === "new")
        .map((e) => e.to),
    );
    const path = seam(s);

    const want: ElementDefinition[] = [];
    for (const n of s.graph.nodes) {
      const p = pos.get(n.id);
      if (!p || n.label === "Agent") continue;
      let st = statusOf(s, n.id);
      if ((n.label === "Source" && flagged && n.id === SOURCE) || poisonedBy.has(n.id))
        st = "poisoned";
      // Untouched actions look plain; outside the blast radius they are dimmed.
      if (st === "ok") st = undefined;
      // An effect the repair created (a drafted reply) is new.
      if (n.label === "Effect" && newBy.has(n.id)) st = "new";
      const label = labelFor(n, s.graph, st);
      const cls = classesFor(n.label, st, n.props);
      if (dimmed(n.id)) cls.push("dim");
      if (path.nodes.has(n.id)) cls.push("on-path");
      if (n.id === s.selected) cls.push("sel");
      want.push({
        group: "nodes",
        data: { id: n.id, label, w: pillWidth(label) },
        position: { x: p.x, y: stretch(p.y, k) },
        classes: cls,
      });
    }
    for (const e of s.graph.edges) {
      if (!DRAWN.has(e.type) || !pos.has(e.from) || !pos.has(e.to)) continue;
      const id = `${e.from}|${e.type}|${e.to}`;
      const cls = [`e-${e.type}`];
      const down =
        e.type === "HANDED_OFF" ? e.to : e.type === "READ" || e.type === "CAUSED" ? e.from : null;
      if (down && REPAIRED.includes(statusOf(s, down) as UiStatus)) cls.push("repaired");
      if (e.type === "HANDED_OFF" && statusOf(s, e.from) === "rerun_same") cls.push("stop");
      if (dimmed(e.from) || dimmed(e.to)) cls.push("dim");
      if (s.selected && (e.from === s.selected || e.to === s.selected)) cls.push("strong");
      if (path.edges.has(id)) cls.push("on-path");
      want.push({ group: "edges", data: { id, source: e.from, target: e.to }, classes: cls });
    }

    cy.batch(() => {
      const keep = new Set(want.map((w) => w.data.id));
      cy.elements().forEach((el) => {
        if (!keep.has(el.id())) cy.remove(el);
      });
      for (const w of want) {
        const el = cy.getElementById(w.data.id!);
        if (el.empty()) {
          cy.add(w);
          continue;
        }
        el.data(w.data);
        el.classes(w.classes as string[]);
        if (w.position) el.position(w.position);
      }
    });

    // Refit when the phase or the canvas size changes, and while the run adds
    // nodes. In between, the viewer's own pan and zoom are left alone.
    const key = `${s.phase}-${s.blast.length}-${size.w - inset}x${size.h}-${s.phase === "running" ? pos.size : 0}`;
    if (key !== fitKey.current && framing) {
      fitKey.current = key;
      cy.viewport({ zoom: framing.zoom, pan: framing.pan });
    }
  }, [s, pos, flagged, ready, size, k, framing, inset]);

  // Lane bands, the kept column and the repair front are HTML over the canvas.
  const toY = (y: number) => y * view.zoom + view.y;
  const toX = (x: number) => x * view.zoom + view.x;
  const frontX = useMemo(() => {
    if (s.front === null) return null;
    const a = s.graph.nodes.find((n) => n.label === "Action" && Number(n.props.seq) === s.front);
    const p = a && pos.get(a.id);
    return p ? p.x + COL_W / 2 : null;
  }, [s.front, s.graph, pos]);

  return (
    <div className="absolute inset-0 overflow-hidden bg-canvas">
      {LANES.map((name, i) => {
        const I = ICONS[LANE_ICON[i]];
        return (
          <div
            key={name}
            className="pointer-events-none absolute inset-x-0 border-t border-rule odd:bg-white/[0.012]"
            style={{ top: toY((i - 0.5) * LANE_H * k), height: LANE_H * k * view.zoom }}
          >
            <span className="absolute top-2 left-3 flex items-center gap-[7px] font-sans text-[12.5px] text-muted">
              <I size={15} className="text-faint" />
              {name}
            </span>
          </div>
        );
      })}
      {kept && kept.count > 0 && (
        <div
          className="pointer-events-none absolute flex items-end justify-center rounded-lg border border-dashed border-line pb-2.5 text-center font-sans text-xs text-faint"
          style={{
            left: toX(kept.x - COL_W / 2 + 8),
            width: (COL_W - 16) * view.zoom,
            top: toY(-0.5 * LANE_H * k) + 8,
            height: LANES.length * LANE_H * k * view.zoom - 16,
          }}
        >
          <span>
            {kept.tickets[0]} to {kept.tickets.at(-1)}
            <br />
            {kept.count} tickets kept
          </span>
        </div>
      )}
      <div className="absolute inset-0">
        <div ref={box} className="size-full" />
      </div>
      {frontX !== null && (
        <div
          className="pointer-events-none absolute inset-y-0 border-l-[1.5px] border-gold transition-[left] duration-200 ease-out"
          style={{ left: toX(frontX) }}
        >
          <span className="absolute top-2 left-1.5 flex items-center gap-[5px] font-sans text-xs whitespace-nowrap text-gold">
            <ArrowRightIcon size={13} />
            repair front
          </span>
        </div>
      )}
    </div>
  );
}
