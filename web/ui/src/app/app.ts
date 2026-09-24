import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
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
}

/** The not-ready count of a list and its worst tone, as the overview ranks them. */
function problems(conditionLists: Condition[][], types: Condition['type'][]): Pick<NavItem, 'problems' | 'tone'> {
  const found = conditionLists.map((c) => firstProblem(c, types)).filter((c): c is Condition => !!c);
  return { problems: found.length, tone: found.some((c) => tone(c) === 'bad') ? 'bad' : 'warn' };
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
    // Counted only once a list has arrived: nothing is known before that.
    const l = this.lists;
    const count = (loaded: boolean, items: { status: { conditions: Condition[] } }[], types: Condition['type'][]) =>
      loaded ? problems(items.map((i) => i.status.conditions), types) : {};
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
