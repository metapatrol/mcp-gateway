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
  selector: 'app-capabilities',
  imports: [RouterLink, ConditionPill, LoadState, ListSearch, SortHeader],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <h1 class="mb-4 flex items-baseline gap-2 text-xl font-semibold tracking-tight">
      Capabilities<span class="font-mono text-[13px] font-normal text-faint">MCPCapability</span>
    </h1>
    <app-load-state [error]="lists.capabilities.error()" [loading]="!lists.capabilities.hasValue()" what="capabilities" />
    @if (lists.capabilities.hasValue()) {
      <section class="panel">
        <header class="panel-head">
          <app-list-search placeholder="Filter by name, description or tool set" [query]="q() ?? ''" [shown]="rows().length" [total]="lists.capabilityItems().length" />
        </header>
        <div class="overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th><app-sort key="name" [sort]="sort()">Name</app-sort></th>
              <th><app-sort key="description" [sort]="sort()">Description</app-sort></th>
              <th><app-sort key="toolsets" [sort]="sort()">Tool sets</app-sort></th>
              <th><app-sort key="valid" [sort]="sort()">Valid</app-sort></th>
              <th><app-sort key="ready" [sort]="sort()">Ready</app-sort></th>
            </tr>
          </thead>
          <tbody>
            @for (c of rows(); track c.meta.name) {
              <tr class="row-link" (click)="open(c.meta.name)">
                <td><a class="link" [routerLink]="['/capabilities', c.meta.name]">{{ c.meta.name }}</a></td>
                <td class="muted text-[13px]">{{ c.spec.description }}</td>
                <td>
                  @for (t of c.spec.toolSets; track t; let last = $last) {
                    @if (lists.toolSet(t)) {
                      <a class="link text-[12.5px]" [routerLink]="['/toolsets', t]" (click)="$event.stopPropagation()">{{ t }}</a>
                    } @else {
                      <span class="font-mono text-[12.5px] text-bad line-through" title="not found">{{ t }}</span>
                    }@if (!last) {, }
                  }
                </td>
                <td><app-condition label="Valid" bare [condition]="get(c.status.conditions, 'Valid')" /></td>
                <td><app-condition label="Ready" bare [condition]="get(c.status.conditions, 'Ready')" /></td>
              </tr>
            } @empty {
              <tr><td colspan="5" class="faint">{{ lists.capabilityItems().length ? 'No capability matches.' : 'No MCPCapability exists.' }}</td></tr>
            }
          </tbody>
        </table>
        </div>
      </section>
    }
  `,
})
export class Capabilities {
  protected readonly lists = inject(Lists);
  private readonly router = inject(Router);
  protected readonly get = (c: Condition[], t: Condition['type']) => condition(c, t);
  /** ?q= and ?sort=. */
  readonly q = input<string>();
  readonly sort = input<string>();
  protected readonly rows = computed(() =>
    sortRows(
      searchRows(this.lists.capabilityItems(), this.q() ?? '', (c) => [c.meta.name, c.spec.description, ...c.spec.toolSets]),
      parseSort(this.sort()),
      {
        name: (c) => c.meta.name,
        description: (c) => c.spec.description || undefined,
        toolsets: (c) => c.spec.toolSets.join(', '),
        valid: (c) => conditionRank(condition(c.status.conditions, 'Valid')),
        ready: (c) => conditionRank(condition(c.status.conditions, 'Ready')),
      },
      (c) => c.meta.name,
    ),
  );

  protected open(name: string): void {
    void this.router.navigate(['/capabilities', name]);
  }
}
