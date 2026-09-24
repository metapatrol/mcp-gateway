import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { condition } from '../../core/conditions';
import { Lists } from '../../core/lists';
import { ago } from '../../core/time';
import { ConditionPill } from '../../shared/condition-pill';
import { LoadState } from '../../shared/load-state';
import { conditionRank, parseSort, searchRows, sortRows } from '../../core/table';
import { ListSearch } from '../../shared/list-search';
import { SortHeader } from '../../shared/sort-header';


@Component({
  selector: 'app-servers',
  imports: [RouterLink, ConditionPill, LoadState, ListSearch, SortHeader],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <h1 class="mb-4 flex items-baseline gap-2 text-xl font-semibold tracking-tight">
      Servers<span class="font-mono text-[13px] font-normal text-faint">MCPServer</span>
    </h1>
    <app-load-state [error]="lists.servers.error()" [loading]="!lists.servers.hasValue()" what="servers" />
    @if (lists.servers.hasValue()) {
      <section class="panel">
        <header class="panel-head">
          <app-list-search placeholder="Filter by name, endpoint or auth" [query]="q() ?? ''" [shown]="rows().length" [total]="lists.serverItems().length" />
        </header>
        <div class="overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th><app-sort key="name" [sort]="sort()">Name</app-sort></th>
              <th><app-sort key="endpoint" [sort]="sort()">Endpoint</app-sort></th>
              <th><app-sort key="auth" [sort]="sort()">Auth</app-sort></th>
              <th class="text-right"><app-sort key="tools" [sort]="sort()">Tools</app-sort></th>
              <th><app-sort key="discovery" [sort]="sort()">Last discovery</app-sort></th>
              <th><app-sort key="ready" [sort]="sort()">Ready</app-sort></th>
            </tr>
          </thead>
          <tbody>
            @for (s of rows(); track s.meta.name) {
              <tr class="row-link" (click)="open(s.meta.name)">
                <td><a class="link" [routerLink]="['/servers', s.meta.name]">{{ s.meta.name }}</a></td>
                <td class="muted font-mono text-[12.5px]">{{ s.spec.endpoint }}</td>
                <td>{{ s.spec.auth.type }}</td>
                <td class="num">{{ s.discovery.toolCount ?? '—' }}</td>
                <td class="muted whitespace-nowrap">{{ since(s.discovery.lastSuccess) }}</td>
                <td><app-condition label="Ready" bare [condition]="ready(s.status.conditions)" /></td>
              </tr>
            } @empty {
              <tr><td colspan="6" class="faint">{{ lists.serverItems().length ? 'No server matches.' : 'No MCPServer exists.' }}</td></tr>
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
