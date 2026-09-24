import { Route, UrlSegment, UrlSegmentGroup } from '@angular/router';
import { describe, expect, it } from 'vitest';
import { detail } from './app.routes';

const match = (matcher: ReturnType<typeof detail>, path: string) => {
  const segments = path.split('/').map((p) => new UrlSegment(p, {}));
  const r = matcher(segments, new UrlSegmentGroup(segments, {}), {} as Route);
  return r && Object.fromEntries(Object.entries(r.posParams ?? {}).map(([k, v]) => [k, v.path]));
};

describe('detail', () => {
  const servers = detail('servers');

  it('matches a page and its MCP view', () => {
    expect(match(servers, 'servers/search')).toEqual({ name: 'search' });
    expect(match(servers, 'servers/search/mcp')).toEqual({ name: 'search', tab: 'mcp' });
  });

  it('takes a resource named mcp for a name', () => {
    expect(match(servers, 'servers/mcp')).toEqual({ name: 'mcp' });
    expect(match(servers, 'servers/mcp/mcp')).toEqual({ name: 'mcp', tab: 'mcp' });
  });

  it('matches nothing else', () => {
    expect(match(servers, 'servers')).toBeNull();
    expect(match(servers, 'servers/search/other')).toBeNull();
    expect(match(servers, 'servers/search/mcp/more')).toBeNull();
    expect(match(servers, 'toolsets/search')).toBeNull();
  });

  it('matches an unnamed page under a longer prefix', () => {
    const subject = detail('subjects/resolve', false);
    expect(match(subject, 'subjects/resolve')).toEqual({});
    expect(match(subject, 'subjects/resolve/mcp')).toEqual({ tab: 'mcp' });
    expect(match(subject, 'subjects')).toBeNull();
  });
});
