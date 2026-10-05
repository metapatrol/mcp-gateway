import { CapabilitySummary, GrantSummary, ServerSummary, ToolSetSummary } from '../api/models';
import { buildGraph, focusOn, matching, related } from './graph';

const meta = (name: string) => ({ name, generation: 1, creationTimestamp: '' });
const ok = [{ type: 'Ready' as const, status: 'True' as const, reason: 'Available', message: '', lastTransitionTime: '' }];

const servers = [{ meta: meta('si'), spec: { endpoint: '', discoveryInterval: '1m', auth: { type: 'None' } }, status: { available: true, conditions: ok } }] as unknown as ServerSummary[];
const toolSets = [
  { meta: meta('si-read'), spec: { serverRef: 'si', tools: ['*'] }, status: { resolvedToolCount: 3, conditions: ok } },
  { meta: meta('other'), spec: { serverRef: 'gone', tools: ['*'] }, status: { resolvedToolCount: 0, conditions: ok } },
] as unknown as ToolSetSummary[];
const capabilities = [{ meta: meta('research'), spec: { toolSets: ['si-read'] }, status: { conditions: ok } }] as unknown as CapabilitySummary[];
const grants = [
  { meta: meta('coder'), spec: { subject: 'agent', capabilities: ['research', 'missing'] }, status: { conditions: ok } },
] as unknown as GrantSummary[];

describe('buildGraph', () => {
  it('draws references, and a node for each that names nothing', () => {
    const g = buildGraph(servers, toolSets, capabilities, grants);
    expect(g.columns.capability.map((n) => [n.name, n.missing])).toEqual([
      ['research', false],
      ['missing', true],
    ]);
    expect(g.columns.server.find((n) => n.name === 'gone')?.missing).toBe(true);
    expect(g.edges.filter((e) => e.broken).map((e) => e.to)).toEqual(['capability:missing', 'server:gone']);
  });

  it('follows a chain both ways from any node', () => {
    const g = buildGraph(servers, toolSets, capabilities, grants);
    expect([...related(g.edges, 'capability:research')].sort()).toEqual(['capability:research', 'grant:coder', 'server:si', 'toolset:si-read']);
    expect(related(g.edges, 'toolset:other').has('grant:coder')).toBe(false);
  });
});

describe('focusOn', () => {
  it('keeps the chains through the seeds, both ways, and nothing else', () => {
    const g = buildGraph(servers, toolSets, capabilities, grants);
    expect([...focusOn(g.edges, ['capability:research'])].sort()).toEqual([...related(g.edges, 'capability:research')].sort());
    expect([...focusOn(g.edges, ['toolset:other'])].sort()).toEqual(['server:gone', 'toolset:other']);
    expect(focusOn(g.edges, []).size).toBe(0);
  });

  it('finds seeds by name or subject', () => {
    const g = buildGraph(servers, toolSets, capabilities, grants);
    expect(matching(g, 'AGENT')).toEqual(['grant:coder']);
    expect(matching(g, 'si-')).toEqual(['toolset:si-read']);
  });
});
