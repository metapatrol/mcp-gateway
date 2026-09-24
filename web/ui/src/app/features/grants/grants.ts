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
  selector: 'app-grants',
  imports: [RouterLink, ConditionPill, LoadState, ListSearch, SortHeader],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <h1 class="mb-4 flex items-baseline gap-2 text-xl font-semibold tracking-tight">
      Grants<span class="font-mono text-[13px] font-normal text-faint">MCPGrant</span>
    </h1>
    <app-load-state [error]="lists.grants.error()" [loading]="!lists.grants.hasValue()" what="grants" />
    @if (lists.grants.hasValue()) {
      <section class="panel">
        <header class="panel-head">
          <app-list-search placeholder="Filter by name, subject or capability" [query]="q() ?? ''" [shown]="rows().length" [total]="lists.grantItems().length" />
        </header>
        <div class="overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th><app-sort key="name" [sort]="sort()">Name</app-sort></th>
              <th><app-sort key="subject" [sort]="sort()">Subject</app-sort></th>
              <th><app-sort key="capabilities" [sort]="sort()">Capabilities</app-sort></th>
              <th><app-sort key="valid" [sort]="sort()">Valid</app-sort></th>
              <th><app-sort key="ready" [sort]="sort()">Ready</app-sort></th>
            </tr>
          </thead>
          <tbody>
            @for (g of rows(); track g.meta.name) {
              <tr class="row-link" (click)="open(g.meta.name)">
                <td><a class="link" [routerLink]="['/grants', g.meta.name]">{{ g.meta.name }}</a></td>
                <td>
                  <a class="link" [routerLink]="['/subjects/resolve']" [queryParams]="{ subject: g.spec.subject }" (click)="$event.stopPropagation()">{{ g.spec.subject }}</a>
                </td>
                <td>
                  @for (c of g.spec.capabilities; track c; let last = $last) {
                    @if (lists.capability(c)) {
                      <a class="link text-[12.5px]" [routerLink]="['/capabilities', c]" (click)="$event.stopPropagation()">{{ c }}</a>
                    } @else {
                      <span class="font-mono text-[12.5px] text-bad line-through" title="not found">{{ c }}</span>
                    }@if (!last) {, }
                  }
                </td>
                <td><app-condition label="Valid" bare [condition]="get(g.status.conditions, 'Valid')" /></td>
                <td><app-condition label="Ready" bare [condition]="get(g.status.conditions, 'Ready')" /></td>
              </tr>
            } @empty {
              <tr><td colspan="5" class="faint">{{ lists.grantItems().length ? 'No grant matches.' : 'No MCPGrant exists.' }}</td></tr>
            }
          </tbody>
        </table>
        </div>
      </section>
    }
  `,
})
export class Grants {
  protected readonly lists = inject(Lists);
  private readonly router = inject(Router);
  protected readonly get = (c: Condition[], t: Condition['type']) => condition(c, t);
  /** ?q= and ?sort=. */
  readonly q = input<string>();
  readonly sort = input<string>();
  protected readonly rows = computed(() =>
    sortRows(
      searchRows(this.lists.grantItems(), this.q() ?? '', (g) => [g.meta.name, g.spec.subject, ...g.spec.capabilities]),
      parseSort(this.sort()),
      {
        name: (g) => g.meta.name,
        subject: (g) => g.spec.subject,
        capabilities: (g) => g.spec.capabilities.join(', '),
        valid: (g) => conditionRank(condition(g.status.conditions, 'Valid')),
        ready: (g) => conditionRank(condition(g.status.conditions, 'Ready')),
      },
      (g) => g.meta.name,
    ),
  );

  protected open(name: string): void {
    void this.router.navigate(['/grants', name]);
  }
}
