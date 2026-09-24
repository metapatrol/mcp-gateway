import { declaredHints } from './tool-doc';

const texts = (a: Parameters<typeof declaredHints>[0]) => declaredHints(a).map((h) => h.text);

describe('declaredHints', () => {
  it('shows nothing for a tool that declares nothing', () => {
    expect(texts(undefined)).toEqual([]);
    // What the gateway passes on for a backend that sent annotations with
    // only a title: the SDK writes the bare booleans as false.
    expect(texts({ title: 'Search', readOnlyHint: false, idempotentHint: false })).toEqual([]);
  });

  it('shows what is declared', () => {
    expect(texts({ readOnlyHint: true, idempotentHint: true, openWorldHint: false })).toEqual(['read-only', 'idempotent', 'closed world']);
    expect(texts({ destructiveHint: true })).toEqual(['destructive']);
    expect(texts({ destructiveHint: false, openWorldHint: true })).toEqual(['not destructive', 'open world']);
  });

  it('ignores destructiveHint on a read-only tool, as MCP does', () => {
    expect(texts({ readOnlyHint: true, destructiveHint: true })).toEqual(['read-only']);
  });
});
