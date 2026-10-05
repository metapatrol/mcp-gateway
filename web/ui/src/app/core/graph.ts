import { CapabilitySummary, GrantSummary, ServerSummary, ToolSetSummary } from '../api/models';
import { CONDITIONS, healthy, SERVER_CONDITIONS } from './conditions';

export type Kind = 'grant' | 'capability' | 'toolset' | 'server';

export interface GraphNode {
  id: string;
  kind: Kind;
  name: string;
  detail?: string;
  healthy: boolean;
  /** Named by a reference, but no resource of that name exists. */
  missing: boolean;
  /** How many healthy boxes this one stands for, when it is a bundle of them. */
  bundle?: number;
}

export interface GraphEdge {
  from: string;
  to: string;
  /** The reference names nothing. */
  broken: boolean;
}

export interface Graph {
  columns: Record<Kind, GraphNode[]>;
  edges: GraphEdge[];
}

const id = (kind: Kind, name: string) => `${kind}:${name}`;

/**
 * The chain grant → capability → tool set → server, as the manifests draw
 * it. A reference to something that does not exist gets a node of its own,
 * marked missing, so a broken link is visible where it breaks.
 */
export function buildGraph(
  servers: ServerSummary[],
  toolSets: ToolSetSummary[],
  capabilities: CapabilitySummary[],
  grants: GrantSummary[],
): Graph {
  const columns: Record<Kind, GraphNode[]> = {
    grant: grants.map((g) => ({
      id: id('grant', g.meta.name),
      kind: 'grant',
      name: g.meta.name,
      detail: g.spec.subject,
      healthy: healthy(g.status.conditions, CONDITIONS),
      missing: false,
    })),
    capability: capabilities.map((c) => ({
      id: id('capability', c.meta.name),
      kind: 'capability',
      name: c.meta.name,
      healthy: healthy(c.status.conditions, CONDITIONS),
      missing: false,
    })),
    toolset: toolSets.map((t) => ({
      id: id('toolset', t.meta.name),
      kind: 'toolset',
      name: t.meta.name,
      detail: `${t.status.resolvedToolCount} ${t.status.resolvedToolCount === 1 ? 'tool' : 'tools'}`,
      healthy: healthy(t.status.conditions, CONDITIONS),
      missing: false,
    })),
    server: servers.map((s) => ({
      id: id('server', s.meta.name),
      kind: 'server',
      name: s.meta.name,
      detail: s.spec.auth.type,
      healthy: healthy(s.status.conditions, SERVER_CONDITIONS),
      missing: false,
    })),
  };
  const edges: GraphEdge[] = [];
  const byId = new Map(Object.values(columns).flatMap((c) => c.map((n) => [n.id, n] as const)));
  const link = (fromKind: Kind, from: string, toKind: Kind, to: string) => {
    const target = id(toKind, to);
    let node = byId.get(target);
    if (!node) {
      node = { id: target, kind: toKind, name: to, healthy: false, missing: true };
      columns[toKind].push(node);
      byId.set(target, node);
    }
    edges.push({ from: id(fromKind, from), to: target, broken: node.missing });
  };
  grants.forEach((g) =>
    g.spec.capabilities.forEach((c) => link('grant', g.meta.name, 'capability', c)),
  );
  capabilities.forEach((c) =>
    c.spec.toolSets.forEach((t) => link('capability', c.meta.name, 'toolset', t)),
  );
  toolSets.forEach((t) => link('toolset', t.meta.name, 'server', t.spec.serverRef));
  return { columns, edges };
}

/** Every node on a path through id: what leads to it and what it leads to. */
export function related(edges: GraphEdge[], start: string): Set<string> {
  const walk = (forward: boolean) => {
    const seen = new Set([start]);
    const queue = [start];
    while (queue.length) {
      const n = queue.shift() as string;
      for (const e of edges) {
        const [from, to] = forward ? [e.from, e.to] : [e.to, e.from];
        if (from === n && !seen.has(to)) {
          seen.add(to);
          queue.push(to);
        }
      }
    }
    return seen;
  };
  return new Set([...walk(true), ...walk(false)]);
}

/**
 * Every node on a path through any of the seeds: the chains they sit on.
 * One walk each way from all seeds at once, so a map of thousands of boxes
 * with hundreds of matches stays cheap.
 */
export function focusOn(edges: GraphEdge[], seeds: Iterable<string>): Set<string> {
  const next = new Map<string, string[]>();
  const prev = new Map<string, string[]>();
  for (const e of edges) {
    next.set(e.from, [...(next.get(e.from) ?? []), e.to]);
    prev.set(e.to, [...(prev.get(e.to) ?? []), e.from]);
  }
  const out = new Set<string>();
  for (const step of [next, prev]) {
    const seen = new Set(seeds);
    const queue = [...seen];
    while (queue.length) {
      const n = queue.pop() as string;
      out.add(n);
      for (const m of step.get(n) ?? []) {
        if (!seen.has(m)) {
          seen.add(m);
          queue.push(m);
        }
      }
    }
  }
  return out;
}

/**
 * Above this many boxes a column shows only those with a problem and bundles
 * the rest into one box. Which kind outnumbers the others is the cluster's
 * business: a thousand servers with five grants, or the other way round.
 */
export const COLUMN_LIMIT = 20;

/**
 * The map as it is drawn: every column of at most limit boxes whole, every
 * longer one with its troubled boxes and one bundle for the rest. Lines to
 * bundled boxes go to the bundle, one per neighbour.
 */
export function condense(graph: Graph, limit = COLUMN_LIMIT): Graph {
  const into = new Map<string, string>();
  const columns = {} as Record<Kind, GraphNode[]>;
  for (const kind of Object.keys(graph.columns) as Kind[]) {
    const nodes = graph.columns[kind];
    if (nodes.length <= limit) {
      columns[kind] = nodes;
      continue;
    }
    const kept = nodes.filter((n) => !n.healthy || n.missing);
    const rest = nodes.length - kept.length;
    columns[kind] = kept;
    if (rest) {
      const bundle: GraphNode = {
        id: `${kind}:*`,
        kind,
        name: `+${rest}`,
        healthy: true,
        missing: false,
        bundle: rest,
      };
      columns[kind] = [...kept, bundle];
      nodes.filter((n) => n.healthy && !n.missing).forEach((n) => into.set(n.id, bundle.id));
    }
  }
  const edges = new Map<string, GraphEdge>();
  for (const e of graph.edges) {
    const from = into.get(e.from) ?? e.from;
    const to = into.get(e.to) ?? e.to;
    const key = `${from}>${to}`;
    const seen = edges.get(key);
    edges.set(key, { from, to, broken: e.broken || !!seen?.broken });
  }
  return { columns, edges: [...edges.values()] };
}

export function routeOf(node: GraphNode): string[] | null {
  if (node.missing) {
    return null;
  }
  const base = {
    grant: 'grants',
    capability: 'capabilities',
    toolset: 'toolsets',
    server: 'servers',
  }[node.kind];
  return node.bundle ? ['/', base] : ['/', base, node.name];
}
