import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { Tip } from './shared/tip';
import { Condition } from './api/models';
import { CONDITIONS, firstProblem, SERVER_CONDITIONS, tone } from './core/conditions';
import { Inspect } from './core/inspect';
import { Lists } from './core/lists';
import { ThemeService } from './core/theme';

interface NavItem {
  path: string;
  label: string;
  /** How many are not Valid and Ready; absent or 0 shows nothing. */
  problems?: number;
  /** bad when one of them is at fault itself, warn when all only pass trouble on. */
  tone?: 'bad' | 'warn';
  /** Which ones, and why, one per line. */
  tip?: string;
}

/** The not-ready count of a list and its worst tone, as the overview ranks them. */
function problems(items: { name: string; conditions: Condition[] }[], types: Condition['type'][]): Pick<NavItem, 'problems' | 'tone' | 'tip'> {
  const found = items
    .map((i) => ({ name: i.name, c: firstProblem(i.conditions, types) }))
    .filter((x): x is { name: string; c: Condition } => !!x.c);
  return {
    problems: found.length,
    tone: found.some((x) => tone(x.c) === 'bad') ? 'bad' : 'warn',
    tip: found.map((x) => `${x.name} · ${x.c.reason}`).join('\n'),
  };
}

@Component({
  selector: 'app-root',
  imports: [RouterOutlet, RouterLink, RouterLinkActive, Tip],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './app.html',
})
export class App {
  protected readonly inspect = inject(Inspect);
  protected readonly theme = inject(ThemeService);

  private readonly lists = inject(Lists);

  protected readonly cluster = computed<NavItem[]>(() => {
    // Counted only once a list has arrived: nothing is known before that.
    const l = this.lists;
    const count = (loaded: boolean, items: { meta: { name: string }; status: { conditions: Condition[] } }[], types: Condition['type'][]) =>
      loaded ? problems(items.map((i) => ({ name: i.meta.name, conditions: i.status.conditions })), types) : {};
    return [
      { path: '/overview', label: 'Overview' },
      { path: '/servers', label: 'Servers', ...count(l.servers.hasValue(), l.serverItems(), SERVER_CONDITIONS) },
      { path: '/toolsets', label: 'Tool sets', ...count(l.toolSets.hasValue(), l.toolSetItems(), CONDITIONS) },
      { path: '/capabilities', label: 'Capabilities', ...count(l.capabilities.hasValue(), l.capabilityItems(), CONDITIONS) },
      { path: '/grants', label: 'Grants', ...count(l.grants.hasValue(), l.grantItems(), CONDITIONS) },
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
