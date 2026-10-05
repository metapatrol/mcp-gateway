import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { RouterLink } from '@angular/router';
import { MpLoadState, MpPageHeader, MpPill } from '@metapatrol/ng';
import { Condition } from '../../api/models';
import { CONDITIONS, firstProblem, healthy, SERVER_CONDITIONS, tone } from '../../core/conditions';
import { Lists } from '../../core/lists';

interface Issue {
  kind: string;
  name: string;
  route: string[];
  condition: Condition;
  tone: 'bad' | 'warn';
}

interface Tile {
  label: string;
  path: string;
  total: number;
  ready: number;
}

@Component({
  selector: 'app-overview',
  imports: [MpPageHeader, RouterLink, MpPill, MpLoadState],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './overview.html',
})
export class Overview {
  protected readonly lists = inject(Lists);

  protected readonly tiles = computed<Tile[]>(() => {
    const l = this.lists;
    const count = <T extends { status: { conditions: Condition[] } }>(
      items: T[],
      types: Condition['type'][],
    ) => ({
      total: items.length,
      ready: items.filter((i) => healthy(i.status.conditions, types)).length,
    });
    return [
      { label: 'Servers', path: '/servers', ...count(l.serverItems(), SERVER_CONDITIONS) },
      { label: 'Tool sets', path: '/toolsets', ...count(l.toolSetItems(), CONDITIONS) },
      { label: 'Capabilities', path: '/capabilities', ...count(l.capabilityItems(), CONDITIONS) },
      { label: 'Grants', path: '/grants', ...count(l.grantItems(), CONDITIONS) },
    ];
  });

  protected readonly issues = computed<Issue[]>(() => {
    const out: Issue[] = [];
    const add = (
      kind: string,
      base: string,
      name: string,
      conditions: Condition[],
      types: Condition['type'][],
    ) => {
      const c = firstProblem(conditions, types);
      if (c) {
        out.push({
          kind,
          name,
          route: ['/', base, name],
          condition: c,
          tone: tone(c) === 'warn' ? 'warn' : 'bad',
        });
      }
    };
    const l = this.lists;
    l.serverItems().forEach((s) =>
      add('server', 'servers', s.meta.name, s.status.conditions, SERVER_CONDITIONS),
    );
    l.toolSetItems().forEach((t) =>
      add('tool set', 'toolsets', t.meta.name, t.status.conditions, CONDITIONS),
    );
    l.capabilityItems().forEach((c) =>
      add('capability', 'capabilities', c.meta.name, c.status.conditions, CONDITIONS),
    );
    l.grantItems().forEach((g) =>
      add('grant', 'grants', g.meta.name, g.status.conditions, CONDITIONS),
    );
    // The resources at fault first, the ones only passing it on after.
    return out.sort((a, b) => Number(a.tone === 'warn') - Number(b.tone === 'warn'));
  });
}
