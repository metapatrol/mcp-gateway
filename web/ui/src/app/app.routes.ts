import { Routes, UrlMatcher, UrlSegment } from '@angular/router';

const title = (page: string) => `${page} · mcp-gateway`;

/**
 * A detail page, and its MCP view one segment further, as in the API:
 * /servers/search and /servers/search/mcp. Both are one route, so switching
 * between them keeps the page and what it has loaded.
 */
export function detail(section: string, named = true): UrlMatcher {
  const prefix = section.split('/');
  return (segments) => {
    if (!prefix.every((p, i) => segments[i]?.path === p)) {
      return null;
    }
    const posParams: Record<string, UrlSegment> = {};
    let at = prefix.length;
    if (named) {
      if (!segments[at]) {
        return null;
      }
      posParams['name'] = segments[at++];
    }
    if (segments[at]?.path === 'mcp') {
      posParams['tab'] = segments[at++];
    }
    return at === segments.length ? { consumed: segments, posParams } : null;
  };
}

export const routes: Routes = [
  { path: '', pathMatch: 'full', redirectTo: 'overview' },
  {
    path: 'overview',
    title: title('Overview'),
    loadComponent: () => import('./features/overview/overview').then((m) => m.Overview),
  },
  {
    path: 'servers',
    title: title('Servers'),
    loadComponent: () => import('./features/servers/servers').then((m) => m.Servers),
  },
  {
    matcher: detail('servers'),
    title: title('Server'),
    loadComponent: () => import('./features/servers/server').then((m) => m.ServerDetail),
  },
  {
    path: 'toolsets',
    title: title('Tool sets'),
    loadComponent: () => import('./features/toolsets/toolsets').then((m) => m.ToolSets),
  },
  {
    matcher: detail('toolsets'),
    title: title('Tool set'),
    loadComponent: () => import('./features/toolsets/toolset').then((m) => m.ToolSetDetail),
  },
  {
    path: 'capabilities',
    title: title('Capabilities'),
    loadComponent: () => import('./features/capabilities/capabilities').then((m) => m.Capabilities),
  },
  {
    matcher: detail('capabilities'),
    title: title('Capability'),
    loadComponent: () => import('./features/capabilities/capability').then((m) => m.CapabilityDetail),
  },
  {
    path: 'grants',
    title: title('Grants'),
    loadComponent: () => import('./features/grants/grants').then((m) => m.Grants),
  },
  {
    matcher: detail('grants'),
    title: title('Grant'),
    loadComponent: () => import('./features/grants/grant').then((m) => m.GrantDetail),
  },
  {
    path: 'subjects',
    title: title('Subjects'),
    loadComponent: () => import('./features/subjects/subjects').then((m) => m.Subjects),
  },
  {
    // The subject is a query parameter, as in the API: a sub claim may be a
    // URL, and does not survive as a path segment.
    matcher: detail('subjects/resolve', false),
    title: title('Subject'),
    loadComponent: () => import('./features/subjects/subject').then((m) => m.SubjectDetail),
  },
  {
    path: '**',
    title: title('Not found'),
    loadComponent: () => import('./features/not-found/not-found').then((m) => m.NotFound),
  },
];
