import { CatalogToolSelection, SelectionExplanation } from '../../api/models';
import { disagreement, filtered, reason, touchedBy } from './selection';

const tool = (name: string, over: Partial<CatalogToolSelection>): CatalogToolSelection => ({
  tool: { name, inputSchema: {} },
  state: 'unselected',
  selectedBy: [],
  excludedBy: [],
  ...over,
});

const tools = [
  tool('code.search', { state: 'selected', exposedAs: 'code.search', selectedBy: ['code.*'] }),
  tool('memory_search', { state: 'selected', exposedAs: 'recall', renamedBy: 'alias', selectedBy: ['memory_*'] }),
  tool('repositories.read_file', { state: 'excluded', selectedBy: ['repositories.*'], excludedBy: ['repositories.read_file'] }),
  tool('admin.reindex', {}),
];
const sel = { tools } as SelectionExplanation;

describe('selection', () => {
  it('explains each state in a sentence', () => {
    expect(reason(tools[0])).toBe('selected by code.*');
    expect(reason(tools[1])).toBe('selected by memory_*, renamed by alias');
    expect(reason(tools[2])).toBe('matched repositories.*, removed by repositories.read_file');
    expect(reason(tools[3])).toBe('no entry of tools matches it');
  });

  it('filters exposed from left out', () => {
    expect(filtered(sel, 'exposed').map((t) => t.tool.name)).toEqual(['code.search', 'memory_search']);
    expect(filtered(sel, 'left-out').map((t) => t.tool.name)).toEqual(['repositories.read_file', 'admin.reindex']);
    expect(filtered(sel, 'all')).toHaveLength(4);
  });

  it('marks the rows an entry decided', () => {
    expect(touchedBy(tools[2], 'repositories.read_file')).toBe(true);
    expect(touchedBy(tools[1], 'alias:memory_search')).toBe(true);
    expect(touchedBy(tools[0], null)).toBe(false);
  });

  it('finds where status and this replica differ', () => {
    expect(disagreement(['code.search', 'recall'], sel)).toEqual([]);
    expect(disagreement(['code.search', 'gone'], sel)).toEqual(['gone', 'recall']);
  });
});
