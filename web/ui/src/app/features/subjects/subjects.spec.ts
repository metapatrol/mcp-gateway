import { SubjectSummary } from '../../api/models';
import { filterSubjects } from './subjects';

const s = (subject: string, grants: string[]): SubjectSummary => ({ subject, grants, toolCount: 0 });
const items = [s('agent-coder', ['coder-agent', 'coder-web']), s('https://idp.example.com/clients/svc-support', ['support-bot'])];

describe('filterSubjects', () => {
  it('matches subjects and their grants, ignoring case', () => {
    expect(filterSubjects(items, 'AGENT').map((x) => x.subject)).toEqual(['agent-coder']);
    expect(filterSubjects(items, 'support').map((x) => x.subject)).toEqual(['https://idp.example.com/clients/svc-support']);
    expect(filterSubjects(items, 'coder-web').map((x) => x.subject)).toEqual(['agent-coder']);
  });

  it('keeps everything for an empty query, and nothing for no match', () => {
    expect(filterSubjects(items, '  ')).toHaveLength(2);
    expect(filterSubjects(items, 'nobody')).toEqual([]);
  });
});
