// Status marks for the provenance graph: node classes, pill labels and the
// Cytoscape stylesheet. Shape and lightness carry status; gold means repaired.

import type { StylesheetJson } from "cytoscape";
import { createElement, type ComponentType } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import {
  DatabaseIcon,
  EnvelopeSimpleIcon,
  GitForkIcon,
  LinkIcon,
  MagnifyingGlassIcon,
  PaperPlaneTiltIcon,
  ReceiptIcon,
  type IconProps,
} from "@phosphor-icons/react";
import type { Graph, GraphNode, Kind } from "@/lib/types";
import type { UiStatus } from "./state";

const C = {
  canvas: "#131217",
  panel: "#18171C",
  raised: "#222127",
  line: "#33323A",
  text: "#E5E5E5",
  muted: "#B2B2B2",
  faint: "#6E6D75",
  gold: "#F5A623",
  goldInk: "#1A1205",
};

export const ICONS: Record<string, ComponentType<IconProps>> = {
  source: LinkIcon,
  research: MagnifyingGlassIcon,
  fact: DatabaseIcon,
  triage: GitForkIcon,
  refund: ReceiptIcon,
  notifier: EnvelopeSimpleIcon,
  effect: PaperPlaneTiltIcon,
};

function iconKey(kind: Kind, props: Record<string, unknown>): string {
  if (kind === "Action") return String(props.tool);
  if (kind === "Effect" && props.kind === "refund") return "refund";
  if (kind === "Effect" && props.kind === "email") return "notifier";
  return kind.toLowerCase();
}

export function classesFor(
  kind: Kind,
  status: UiStatus | undefined,
  props: Record<string, unknown>,
): string[] {
  const out = [`kind-${kind.toLowerCase()}`, `icon-${iconKey(kind, props)}`];
  if (status) out.push(`st-${status}`);
  if (kind === "Fact" && props.active === false) out.push("inactive");
  if (kind === "Effect" && props.sent && !props.live) out.push("undone");
  if (kind === "Source" && props.trust === "bad") out.push("bad");
  return out;
}

// Canvas text has no line-through, so struck labels use a combining stroke.
const STROKE = String.fromCharCode(0x336);
const strike = (s: string) => Array.from(s, (c) => c + STROKE).join("");

// labelFor is the text on a pill: which ticket, which fact, how much.
export function labelFor(n: GraphNode, g: Graph, status?: UiStatus): string {
  const p = n.props;
  let text: string;
  switch (n.label) {
    case "Source":
      text = n.id;
      break;
    case "Fact":
      text = `${String(p.key)} v${String(p.version)}`;
      break;
    case "Effect":
      text = p.kind === "refund" ? `$${String(p.amount)}` : String(p.kind);
      break;
    default: {
      if (p.ticket_id) {
        text = p.ticket_id as string;
        break;
      }
      const src = g.edges.find((e) => e.from === n.id && e.type === "INGESTED")?.to ?? "research";
      text = src.split("/")[0];
    }
  }
  const struck = status === "superseded" || (n.label === "Effect" && p.sent && !p.live);
  return struck ? strike(text) : text;
}

const CHAR_W = 6.9; // Fragment Mono at 11.5px
export function pillWidth(label: string): number {
  return Math.round(30 + label.replaceAll(STROKE, "").length * CHAR_W);
}

const svgCache = new Map<string, string>();
function iconURI(key: string, color: string): string {
  const id = `${key}/${color}`;
  let uri = svgCache.get(id);
  if (!uri) {
    const svg = renderToStaticMarkup(createElement(ICONS[key], { size: 24, color }));
    uri = `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`;
    svgCache.set(id, uri);
  }
  return uri;
}

// Icon colour follows the pill's text colour for each mark.
const ICON_TONES: [string, string][] = [
  ["", C.muted],
  [".st-ok", C.muted],
  [".st-rerun_same", C.text],
  [".st-skipped", C.muted],
  [".st-superseded", C.faint],
  [".st-pending", C.faint],
  [".inactive", C.faint],
  [".undone", C.faint],
  [".st-poisoned", C.panel],
  [".bad", C.panel],
  [".st-rerun_changed", C.goldInk],
  [".st-new", C.gold],
  [".st-running", C.gold],
];

export function stylesheet(): StylesheetJson {
  const sheet: StylesheetJson = [
    {
      selector: "node",
      style: {
        shape: "round-rectangle",
        "corner-radius": "6px",
        width: "data(w)",
        height: 24,
        "background-color": C.panel,
        "border-width": 1,
        "border-color": C.line,
        label: "data(label)",
        color: C.muted,
        "font-family": "Fragment Mono, ui-monospace, monospace",
        "font-size": 11.5,
        "text-valign": "center",
        "text-halign": "center",
        "text-margin-x": 8,
        "background-width": 12,
        "background-height": 12,
        "background-position-x": 9,
        "background-position-y": "50%",
        "background-fit": "none",
        "background-clip": "none",
        "transition-property": "background-color, border-color, color, opacity",
        "transition-duration": 180,
        "transition-timing-function": "ease-out",
      },
    },
    { selector: ".kind-fact", style: { "corner-radius": "12px" } },
    { selector: ".kind-source, .kind-fact", style: { color: C.text } },
    { selector: ".st-ok", style: { opacity: 0.45 } },
    {
      selector: ".st-poisoned, .bad",
      style: { "background-color": C.text, "border-color": C.text, color: C.panel },
    },
    {
      selector: ".st-rerun_changed",
      style: { "background-color": C.gold, "border-color": C.gold, color: C.goldInk },
    },
    {
      selector: ".st-rerun_same",
      style: { "background-color": C.raised, "border-color": C.muted, color: C.text },
    },
    { selector: ".st-skipped", style: { color: C.muted } },
    { selector: ".st-superseded", style: { "border-style": "dashed", color: C.faint } },
    { selector: ".st-new", style: { "border-width": 1.5, "border-color": C.gold, color: C.gold } },
    {
      selector: ".st-running",
      style: {
        "border-color": C.gold,
        color: C.gold,
        "underlay-color": C.gold,
        "underlay-opacity": 0.18,
        "underlay-padding": 3,
      },
    },
    { selector: ".st-pending", style: { "border-style": "dashed", color: C.faint } },
    { selector: ".inactive, .undone", style: { "border-style": "dashed", color: C.faint } },
  ];
  for (const key of Object.keys(ICONS)) {
    for (const [cls, color] of ICON_TONES) {
      sheet.push({
        selector: `.icon-${key}${cls}`,
        style: { "background-image": iconURI(key, color) },
      });
    }
  }
  sheet.push(
    {
      selector: "edge",
      style: {
        width: 1,
        "line-color": C.faint,
        "curve-style": "taxi",
        "taxi-direction": "downward",
        "taxi-radius": 14,
        "target-arrow-shape": "none",
        "transition-property": "line-color, opacity, width",
        "transition-duration": 180,
      },
    },
    { selector: ".e-INGESTED, .e-READ, .e-WROTE, .e-CAUSED", style: { "line-color": C.line } },
    { selector: "edge.strong", style: { "line-color": C.muted } },
    {
      selector: "edge.stop",
      style: { "source-arrow-shape": "tee", "source-arrow-color": C.text, "arrow-scale": 1.3 },
    },
    { selector: "edge.repaired", style: { "line-color": C.gold, width: 1.4, opacity: 0.75 } },
    { selector: ".dim", style: { opacity: 0.3 } },
    { selector: "edge.dim", style: { opacity: 0.15 } },
    {
      selector: "node.sel",
      style: { "underlay-color": C.text, "underlay-opacity": 1, "underlay-padding": 3 },
    },
    { selector: "node.on-path", style: { opacity: 1, "border-color": C.gold } },
    {
      selector: "edge.on-path",
      style: { "line-color": C.gold, width: 2.6, opacity: 1, "z-index": 10 },
    },
  );
  return sheet;
}
