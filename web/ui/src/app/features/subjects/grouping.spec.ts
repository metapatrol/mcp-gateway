import { SubjectTool } from '../../api/models';
import { byServer, notOffered } from './grouping';

const tool = (name: string, server: string, state: SubjectTool['state'] = 'offered'): SubjectTool => ({
  name,
  state,
  claimants: [{ ref: { server, tool: name }, paths: [] }],
});

describe('subject grouping', () => {
  const tools = [tool('search', 'weather'), tool('code.search', 'si'), tool('get_alerts', 'weather'), tool('fetch_page', 'search', 'unavailable')];

  it('groups offered tools by server, sorted', () => {
    expect(byServer(tools).map((g) => [g.server, g.tools.map((t) => t.name)])).toEqual([
      ['si', ['code.search']],
      ['weather', ['get_alerts', 'search']],
    ]);
  });

  it('keeps the rest apart', () => {
    expect(notOffered(tools).map((t) => t.name)).toEqual(['fetch_page']);
  });
});
