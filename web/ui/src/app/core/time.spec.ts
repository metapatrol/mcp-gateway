import { ago, shortRevision } from './time';

describe('time', () => {
  const now = Date.parse('2026-09-24T12:00:00Z');
  it('says how long ago', () => {
    expect(ago('2026-09-24T11:59:18Z', now)).toBe('42s ago');
    expect(ago('2026-09-24T08:48:00Z', now)).toBe('3h 12m ago');
    expect(ago('2026-09-20T12:00:00Z', now)).toBe('4d ago');
    expect(ago(undefined, now)).toBe('—');
  });
  it('shortens revisions', () => {
    expect(shortRevision('sha256:3f9a1c07e2b4aaaaaaaa')).toBe('sha256:3f9a1c07e2b4');
    expect(shortRevision('abcdef0123456789')).toBe('abcdef012345');
  });
});
