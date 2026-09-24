import { conditionRank, formatSort, nextSort, parseSort, searchRows, sortRows } from './table';

interface Row {
  name: string;
  tools?: number;
  server: string;
}
const rows: Row[] = [
  { name: 'tool-10', tools: 3, server: 'b' },
  { name: 'tool-2', tools: 12, server: 'a' },
  { name: 'tool-1', server: 'a' },
];
const columns = { name: (r: Row) => r.name, tools: (r: Row) => r.tools, server: (r: Row) => r.server };
const names = (rs: Row[]) => rs.map((r) => r.name);
const sorted = (param?: string) => names(sortRows(rows, parseSort(param), columns, (r) => r.name));

describe('sortRows', () => {
  it('sorts by name by default, naturally', () => {
    expect(sorted()).toEqual(['tool-1', 'tool-2', 'tool-10']);
    expect(sorted('-name')).toEqual(['tool-10', 'tool-2', 'tool-1']);
  });

  it('compares numbers as numbers and puts missing values last either way', () => {
    expect(sorted('tools')).toEqual(['tool-10', 'tool-2', 'tool-1']);
    expect(sorted('-tools')).toEqual(['tool-2', 'tool-10', 'tool-1']);
  });

  it('breaks ties by name', () => {
    expect(sorted('server')).toEqual(['tool-1', 'tool-2', 'tool-10']);
    expect(sorted('-server')).toEqual(['tool-10', 'tool-1', 'tool-2']);
  });

  it('falls back to the name for an unknown column', () => {
    expect(sorted('nonsense')).toEqual(['tool-1', 'tool-2', 'tool-10']);
  });
});

describe('sort parameter', () => {
  it('leaves the default out of the URL', () => {
    expect(formatSort(parseSort(undefined))).toBeNull();
    expect(formatSort({ key: 'name', desc: true })).toBe('-name');
    expect(formatSort({ key: 'tools', desc: false })).toBe('tools');
  });

  it('turns a column around on a second click', () => {
    expect(nextSort(parseSort(undefined), 'tools')).toEqual({ key: 'tools', desc: false });
    expect(nextSort(parseSort('tools'), 'tools')).toEqual({ key: 'tools', desc: true });
    expect(nextSort(parseSort('-tools'), 'name')).toEqual({ key: 'name', desc: false });
  });
});

describe('searchRows', () => {
  it('matches any text, ignoring case', () => {
    expect(names(searchRows(rows, 'TOOL-1', (r) => [r.name]))).toEqual(['tool-10', 'tool-1']);
    expect(names(searchRows(rows, 'b', (r) => [r.name, r.server]))).toEqual(['tool-10']);
    expect(searchRows(rows, ' ', (r) => [r.name])).toHaveLength(3);
  });
});

describe('conditionRank', () => {
  it('puts what is broken first', () => {
    const c = (status: 'True' | 'False' | 'Unknown') => ({ type: 'Ready', status }) as never;
    expect([conditionRank(c('False')), conditionRank(undefined), conditionRank(c('True'))]).toEqual([0, 1, 2]);
  });
});
