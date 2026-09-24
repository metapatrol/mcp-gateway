import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { Condition } from '../../api/models';
import { condition } from '../../core/conditions';
import { Lists } from '../../core/lists';
import { ConditionPill } from '../../shared/condition-pill';
import { LoadState } from '../../shared/load-state';
import { conditionRank, parseSort, searchRows, sortRows } from '../../core/table';
import { ListSearch } from '../../shared/list-search';
import { SortHeader } from '../../shared/sort-header';


@Component({
  selector: 'app-toolsets',
  imports: [RouterLink, ConditionPill, LoadState, ListSearch, SortHeader],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <h1 class="mb-4 flex items-baseline gap-2 text-xl font-semibold tracking-tight">
      Tool sets<span class="font-mono text-[13px] font-normal text-faint">MCPToolSet</span>
    </h1>
    <app-load-state [error]="lists.toolSets.error()" [loading]="!lists.toolSets.hasValue()" what="tool sets" />
    @if (lists.toolSets.hasValue()) {
      <section class="panel">
        <header class="panel-head">
          <app-list-search placeholder="Filter by name, server or tool" [query]="q() ?? ''" [shown]="rows().length" [total]="lists.toolSetItems().length" />
        </header>
        <div class="overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th><app-sort key="name" [sort]="sort()">Name</app-sort></th>
              <th><app-sort key="server" [sort]="sort()">Server</app-sort></th>
              <th><app-sort key="selection" [sort]="sort()">Selection</app-sort></th>
              <th class="text-right"><app-sort key="tools" [sort]="sort()">Tools</app-sort></th>
              <th><app-sort key="valid" [sort]="sort()">Valid</app-sort></th>
              <th><app-sort key="ready" [sort]="sort()">Ready</app-sort></th>
            </tr>
          </thead>
          <tbody>
            @for (t of rows(); track t.meta.name) {
              <tr class="row-link" (click)="open(t.meta.name)">
                <td><a class="link" [routerLink]="['/toolsets', t.meta.name]">{{ t.meta.name }}</a></td>
                <td>
                  <a class="link text-[12.5px]" [routerLink]="['/servers', t.spec.serverRef]" (click)="$event.stopPropagation()">{{ t.spec.serverRef }}</a>
                </td>
                <td class="muted font-mono text-[12.5px]">
                  {{ t.spec.tools.join(', ') }}
                  @if (t.spec.exclude?.length) {
                    <span class="faint"> − {{ t.spec.exclude.join(', ') }}</span>
                  }
                  @if (t.spec.prefix) {
                    <span class="faint"> · prefix {{ t.spec.prefix }}</span>
                  }
                </td>
                <td class="num">{{ t.status.resolvedToolCount }}</td>
                <td><app-condition label="Valid" bare [condition]="get(t.status.conditions, 'Valid')" /></td>
                <td><app-condition label="Ready" bare [condition]="get(t.status.conditions, 'Ready')" /></td>
              </tr>
            } @empty {
              <tr><td colspan="6" class="faint">{{ lists.toolSetItems().length ? 'No tool set matches.' : 'No MCPToolSet exists.' }}</td></tr>
            }
          </tbody>
        </table>
        </div>
      </section>
    }
  `,
})
export class ToolSets {
  protected readonly lists = inject(Lists);
  private readonly router = inject(Router);
  protected readonly get = (c: Condition[], t: Condition['type']) => condition(c, t);
  /** ?q= and ?sort=. */
  readonly q = input<string>();
  readonly sort = input<string>();
  protected readonly rows = computed(() =>
    sortRows(
      searchRows(this.lists.toolSetItems(), this.q() ?? '', (t) => [
        t.meta.name,
        t.spec.serverRef,
        t.spec.prefix,
        ...t.spec.tools,
        ...(t.spec.exclude ?? []),
      ]),
      parseSort(this.sort()),
      {
        name: (t) => t.meta.name,
        server: (t) => t.spec.serverRef,
        selection: (t) => t.spec.tools.join(', '),
        tools: (t) => t.status.resolvedToolCount ?? undefined,
        valid: (t) => conditionRank(condition(t.status.conditions, 'Valid')),
        ready: (t) => conditionRank(condition(t.status.conditions, 'Ready')),
      },
      (t) => t.meta.name,
    ),
  );

  protected open(name: string): void {
    void this.router.navigate(['/toolsets', name]);
  }
}
