import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { CONDITIONS, healthy, SERVER_CONDITIONS } from './core/conditions';
import { Inspect } from './core/inspect';
import { Lists } from './core/lists';
import { ThemeService } from './core/theme';

interface NavItem {
  path: string;
  label: string;
  count?: number;
  troubled?: boolean;
}

@Component({
  selector: 'app-root',
  imports: [RouterOutlet, RouterLink, RouterLinkActive],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './app.html',
})
export class App {
  protected readonly inspect = inject(Inspect);
  protected readonly theme = inject(ThemeService);

  private readonly lists = inject(Lists);

  protected readonly cluster = computed<NavItem[]>(() => {
    // Counts only once a list has arrived: a zero before that would be a lie.
    const l = this.lists;
    const s = l.servers.hasValue() ? l.serverItems() : undefined;
    const t = l.toolSets.hasValue() ? l.toolSetItems() : undefined;
    const c = l.capabilities.hasValue() ? l.capabilityItems() : undefined;
    const g = l.grants.hasValue() ? l.grantItems() : undefined;
    return [
      { path: '/overview', label: 'Overview' },
      { path: '/servers', label: 'Servers', count: s?.length, troubled: s?.some((x) => !healthy(x.status.conditions, SERVER_CONDITIONS)) },
      { path: '/toolsets', label: 'Tool sets', count: t?.length, troubled: t?.some((x) => !healthy(x.status.conditions, CONDITIONS)) },
      { path: '/capabilities', label: 'Capabilities', count: c?.length, troubled: c?.some((x) => !healthy(x.status.conditions, CONDITIONS)) },
      { path: '/grants', label: 'Grants', count: g?.length, troubled: g?.some((x) => !healthy(x.status.conditions, CONDITIONS)) },
    ];
  });

  /** Whether the console hears about changes, in one word. */
  protected readonly status = computed(() => {
    if (this.lists.servers.error() !== undefined) {
      return { text: 'Gateway unreachable', classes: 'text-bad', pulse: false };
    }
    switch (this.inspect.liveness()) {
      case 'live':
        return { text: 'Live', classes: 'text-ok', pulse: true };
      case 'reconnecting':
        return { text: 'Reconnecting', classes: 'text-warn', pulse: false };
      case 'polling':
        return { text: 'Polling', classes: 'text-muted', pulse: false };
      default:
        return { text: 'Connecting', classes: 'text-faint', pulse: false };
    }
  });
}
