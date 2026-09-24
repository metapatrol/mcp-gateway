import { Routes } from '@angular/router';

const title = (page: string) => `${page} · mcp-gateway`;

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
    path: 'servers/:name',
    title: title('Server'),
    loadComponent: () => import('./features/servers/server').then((m) => m.ServerDetail),
  },
  {
    path: 'toolsets',
    title: title('Tool sets'),
    loadComponent: () => import('./features/toolsets/toolsets').then((m) => m.ToolSets),
  },
  {
    path: 'toolsets/:name',
    title: title('Tool set'),
    loadComponent: () => import('./features/toolsets/toolset').then((m) => m.ToolSetDetail),
  },
  {
    path: 'capabilities',
    title: title('Capabilities'),
    loadComponent: () => import('./features/capabilities/capabilities').then((m) => m.Capabilities),
  },
  {
    path: 'capabilities/:name',
    title: title('Capability'),
    loadComponent: () => import('./features/capabilities/capability').then((m) => m.CapabilityDetail),
  },
  {
    path: 'grants',
    title: title('Grants'),
    loadComponent: () => import('./features/grants/grants').then((m) => m.Grants),
  },
  {
    path: 'grants/:name',
    title: title('Grant'),
    loadComponent: () => import('./features/grants/grant').then((m) => m.GrantDetail),
  },
  {
    // The subject is a query parameter, as in the API: a sub claim may be a
    // URL, and does not survive as a path segment.
    path: 'subjects',
    title: title('Subjects'),
    loadComponent: () => import('./features/subjects/subjects').then((m) => m.Subjects),
  },
  {
    path: '**',
    title: title('Not found'),
    loadComponent: () => import('./features/not-found/not-found').then((m) => m.NotFound),
  },
];
