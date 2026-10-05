import { subjectSort } from './subjects';

describe('subjectSort', () => {
  it('passes the columns of the table on, either way round', () => {
    expect(subjectSort('tools')).toBe('tools');
    expect(subjectSort('-grants')).toBe('-grants');
  });

  it('leaves out what the API would refuse, so the default applies', () => {
    expect(subjectSort(undefined)).toBeUndefined();
    expect(subjectSort('colour')).toBeUndefined();
    expect(subjectSort('--name')).toBeUndefined();
  });
});
