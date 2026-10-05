import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { RouterOutlet } from '@angular/router';
import {
  MpAppShell,
  MpNavBadge,
  MpNavGroup,
  MpShellStatus,
  MpStatus,
  MpStatusKind,
} from '@metapatrol/ng';
import { Condition } from './api/models';
import { CONDITIONS, firstProblem, SERVER_CONDITIONS, tone } from './core/conditions';
import { Inspect } from './core/inspect';
import { Lists } from './core/lists';

/** The not-ready count of a list and its worst tone, as the overview ranks them. */
function problems(
  items: { name: string; conditions: Condition[] }[],
  types: Condition['type'][],
): MpNavBadge {
  const found = items
    .map((i) => ({ name: i.name, c: firstProblem(i.conditions, types) }))
    .filter((x): x is { name: string; c: Condition } => !!x.c);
  return {
    count: found.length,
    tone: found.some((x) => tone(x.c) === 'bad') ? 'bad' : 'warn',
    tip: found.map((x) => `${x.name} · ${x.c.reason}`).join('\n'),
    label: 'not ready',
  };
}

@Component({
  selector: 'app-root',
  imports: [RouterOutlet, MpAppShell, MpShellStatus, MpStatus],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './app.html',
})
export class App {
  protected readonly inspect = inject(Inspect);

  private readonly lists = inject(Lists);

  protected readonly nav = computed<MpNavGroup[]>(() => {
    // Counted only once a list has arrived: nothing is known before that.
    const l = this.lists;
    const count = (
      loaded: boolean,
      items: { meta: { name: string }; status: { conditions: Condition[] } }[],
      types: Condition['type'][],
    ) =>
      loaded
        ? problems(
            items.map((i) => ({ name: i.meta.name, conditions: i.status.conditions })),
            types,
          )
        : undefined;
    return [
      {
        label: 'cluster',
        items: [
          { path: '/overview', label: 'Overview', icon: 'layout-dashboard' },
          { path: '/map', label: 'Map', icon: 'network' },
          {
            path: '/servers',
            label: 'Servers',
            icon: 'server',
            badge: count(l.servers.hasValue(), l.serverItems(), SERVER_CONDITIONS),
          },
          {
            path: '/toolsets',
            label: 'Tool sets',
            icon: 'wrench',
            badge: count(l.toolSets.hasValue(), l.toolSetItems(), CONDITIONS),
          },
          {
            path: '/capabilities',
            label: 'Capabilities',
            icon: 'shield-check',
            badge: count(l.capabilities.hasValue(), l.capabilityItems(), CONDITIONS),
          },
          {
            path: '/grants',
            label: 'Grants',
            icon: 'key-round',
            badge: count(l.grants.hasValue(), l.grantItems(), CONDITIONS),
          },
        ],
      },
      { label: 'access', items: [{ path: '/subjects', label: 'Subjects', icon: 'users' }] },
    ];
  });

  /** Whether the console hears about changes, in one word. */
  protected readonly status = computed<{ text: string; status: MpStatusKind }>(() => {
    if (this.lists.servers.error() !== undefined) {
      return { text: 'Gateway unreachable', status: 'error' };
    }
    switch (this.inspect.liveness()) {
      case 'live':
        return { text: 'Live', status: 'success' };
      case 'reconnecting':
        return { text: 'Reconnecting', status: 'warning' };
      case 'polling':
        return { text: 'Polling', status: 'info' };
      default:
        return { text: 'Connecting', status: 'idle' };
    }
  });
}
