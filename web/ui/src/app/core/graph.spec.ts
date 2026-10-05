import { CapabilitySummary, GrantSummary, ServerSummary, ToolSetSummary } from '../api/models';
import { buildGraph, COLUMN_LIMIT, condense, focusOn, related } from './graph';

const meta = (name: string) => ({ name, generation: 1, creationTimestamp: '' });
const ok = [
  {
    type: 'Ready' as const,
    status: 'True' as const,
    reason: 'Available',
    message: '',
    lastTransitionTime: '',
  },
];

const servers = [
  {
    meta: meta('si'),
    spec: { endpoint: '', discoveryInterval: '1m', auth: { type: 'None' } },
    status: { available: true, conditions: ok },
  },
] as unknown as ServerSummary[];
const toolSets = [
  {
    meta: meta('si-read'),
    spec: { serverRef: 'si', tools: ['*'] },
    status: { resolvedToolCount: 3, conditions: ok },
  },
  {
    meta: meta('other'),
    spec: { serverRef: 'gone', tools: ['*'] },
    status: { resolvedToolCount: 0, conditions: ok },
  },
] as unknown as ToolSetSummary[];
const capabilities = [
  { meta: meta('research'), spec: { toolSets: ['si-read'] }, status: { conditions: ok } },
] as unknown as CapabilitySummary[];
const grants = [
  {
    meta: meta('coder'),
    spec: { subject: 'agent', capabilities: ['research', 'missing'] },
    status: { conditions: ok },
  },
] as unknown as GrantSummary[];

describe('buildGraph', () => {
  it('draws references, and a node for each that names nothing', () => {
    const g = buildGraph(servers, toolSets, capabilities, grants);
    expect(g.columns.capability.map((n) => [n.name, n.missing])).toEqual([
      ['research', false],
      ['missing', true],
    ]);
    expect(g.columns.server.find((n) => n.name === 'gone')?.missing).toBe(true);
    expect(g.edges.filter((e) => e.broken).map((e) => e.to)).toEqual([
      'capability:missing',
      'server:gone',
    ]);
  });

  it('follows a chain both ways from any node', () => {
    const g = buildGraph(servers, toolSets, capabilities, grants);
    expect([...related(g.edges, 'capability:research')].sort()).toEqual([
      'capability:research',
      'grant:coder',
      'server:si',
      'toolset:si-read',
    ]);
    expect(related(g.edges, 'toolset:other').has('grant:coder')).toBe(false);
  });
});

describe('focusOn', () => {
  it('keeps the chains through the seeds, both ways, and nothing else', () => {
    const g = buildGraph(servers, toolSets, capabilities, grants);
    expect([...focusOn(g.edges, ['capability:research'])].sort()).toEqual(
      [...related(g.edges, 'capability:research')].sort(),
    );
    expect([...focusOn(g.edges, ['toolset:other'])].sort()).toEqual([
      'server:gone',
      'toolset:other',
    ]);
    expect(focusOn(g.edges, []).size).toBe(0);
  });
});

describe('condense', () => {
  const both = [...ok, { ...ok[0], type: 'Valid' as const }];
  const many = (n: number, broken = 0) =>
    Array.from({ length: n }, (_, i) => ({
      meta: meta(`g${i}`),
      spec: { subject: `s${i}`, capabilities: i < broken ? ['missing'] : ['research'] },
      status: { conditions: i < broken ? [] : both },
    })) as unknown as GrantSummary[];
  const serversOf = (n: number) =>
    Array.from({ length: n }, (_, i) => ({
      meta: meta(`srv${i}`),
      spec: { endpoint: '', discoveryInterval: '1m', auth: { type: 'None' } },
      status: { available: true, conditions: ok },
    })) as unknown as ServerSummary[];

  it('leaves columns of up to the limit whole', () => {
    const g = buildGraph(servers, toolSets, capabilities, many(COLUMN_LIMIT, 1));
    expect(condense(g)).toEqual(g);
  });

  it('bundles the healthy boxes of a long column, and its lines with them', () => {
    const c = condense(buildGraph(servers, toolSets, capabilities, many(COLUMN_LIMIT + 30, 2)));
    expect(c.columns.grant.map((n) => [n.name, n.bundle])).toEqual([
      ['g0', undefined],
      ['g1', undefined],
      [`+${COLUMN_LIMIT + 28}`, COLUMN_LIMIT + 28],
    ]);
    expect(c.edges.filter((e) => e.from === 'grant:*')).toEqual([
      { from: 'grant:*', to: 'capability:research', broken: false },
    ]);
  });

  it('does so for any kind: many servers, few grants', () => {
    const c = condense(buildGraph(serversOf(1000), toolSets, capabilities, grants));
    // si and gone are named by tool sets but are none of the thousand: missing, so shown.
    expect(c.columns.server.map((n) => n.name)).toEqual(['si', 'gone', '+1000']);
    expect(c.columns.grant).toHaveLength(1);
  });
});
