import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { ago, conditionRank, MpCondition, MpListSearch, MpLoadState, MpPageHeader, MpSort, parseSort, searchRows, sortRows } from '@metapatrol/ng';
import { condition } from '../../core/conditions';
import { Lists } from '../../core/lists';


@Component({
  selector: 'app-servers',
  imports: [MpPageHeader, RouterLink, MpCondition, MpLoadState, MpListSearch, MpSort],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <mp-page-header title="Servers" note="MCPServer" />
    <mp-load-state [error]="lists.servers.error()" [loading]="!lists.servers.hasValue()" what="servers" />
    @if (lists.servers.hasValue()) {
      <section class="mp-panel mp-panel-flush">
        <header class="mp-panel-head">
          <mp-list-search placeholder="Filter by name, endpoint or auth" [query]="q() ?? ''" [shown]="rows().length" [total]="lists.serverItems().length" />
        </header>
        <div class="overflow-x-auto">
        <table class="mp-table">
          <thead>
            <tr>
              <th><mp-sort key="name" [sort]="sort()">Name</mp-sort></th>
              <th><mp-sort key="endpoint" [sort]="sort()">Endpoint</mp-sort></th>
              <th><mp-sort key="auth" [sort]="sort()">Auth</mp-sort></th>
              <th class="text-right"><mp-sort key="tools" [sort]="sort()">Tools</mp-sort></th>
              <th><mp-sort key="discovery" [sort]="sort()">Last discovery</mp-sort></th>
              <th><mp-sort key="ready" [sort]="sort()">Ready</mp-sort></th>
            </tr>
          </thead>
          <tbody>
            @for (s of rows(); track s.meta.name) {
              <tr class="mp-row-link" (click)="open(s.meta.name)">
                <td><a class="mp-link" [routerLink]="['/servers', s.meta.name]">{{ s.meta.name }}</a></td>
                <td class="text-fg-muted font-mono text-[12.5px]">{{ s.spec.endpoint }}</td>
                <td>{{ s.spec.auth.type }}</td>
                <td class="mp-num">{{ s.discovery.toolCount ?? '—' }}</td>
                <td class="text-fg-muted whitespace-nowrap">{{ since(s.discovery.lastSuccess) }}</td>
                <td><mp-condition label="Ready" bare [condition]="ready(s.status.conditions)" /></td>
              </tr>
            } @empty {
              <tr><td colspan="6" class="text-fg-subtle">{{ lists.serverItems().length ? 'No server matches.' : 'No MCPServer exists.' }}</td></tr>
            }
          </tbody>
        </table>
        </div>
      </section>
    }
  `,
})
export class Servers {
  protected readonly lists = inject(Lists);
  private readonly router = inject(Router);
  protected readonly since = ago;
  /** ?q= and ?sort=. */
  readonly q = input<string>();
  readonly sort = input<string>();
  protected readonly rows = computed(() =>
    sortRows(
      searchRows(this.lists.serverItems(), this.q() ?? '', (s) => [s.meta.name, s.spec.endpoint, s.spec.auth.type]),
      parseSort(this.sort()),
      {
        name: (s) => s.meta.name,
        endpoint: (s) => s.spec.endpoint,
        auth: (s) => s.spec.auth.type,
        tools: (s) => s.discovery.toolCount ?? undefined,
        discovery: (s) => (s.discovery.lastSuccess ? Date.parse(s.discovery.lastSuccess) : undefined),
        ready: (s) => conditionRank(condition(s.status.conditions, 'Ready')),
      },
      (s) => s.meta.name,
    ),
  );
  protected readonly ready = (c: Parameters<typeof condition>[0]) => condition(c, 'Ready');

  protected open(name: string): void {
    void this.router.navigate(['/servers', name]);
  }
}
