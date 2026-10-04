import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { conditionRank, MpCondition, MpListSearch, MpLoadState, MpPageHeader, MpSort, parseSort, searchRows, sortRows } from '@metapatrol/ng';
import { Condition } from '../../api/models';
import { condition } from '../../core/conditions';
import { Lists } from '../../core/lists';


@Component({
  selector: 'app-grants',
  imports: [MpPageHeader, RouterLink, MpCondition, MpLoadState, MpListSearch, MpSort],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <mp-page-header title="Grants" note="MCPGrant" />
    <mp-load-state [error]="lists.grants.error()" [loading]="!lists.grants.hasValue()" what="grants" />
    @if (lists.grants.hasValue()) {
      <section class="mp-panel mp-panel-flush">
        <header class="mp-panel-head">
          <mp-list-search placeholder="Filter by name, subject or capability" [query]="q() ?? ''" [shown]="rows().length" [total]="lists.grantItems().length" />
        </header>
        <div class="overflow-x-auto">
        <table class="mp-table">
          <thead>
            <tr>
              <th><mp-sort key="name" [sort]="sort()">Name</mp-sort></th>
              <th><mp-sort key="subject" [sort]="sort()">Subject</mp-sort></th>
              <th><mp-sort key="capabilities" [sort]="sort()">Capabilities</mp-sort></th>
              <th><mp-sort key="valid" [sort]="sort()">Valid</mp-sort></th>
              <th><mp-sort key="ready" [sort]="sort()">Ready</mp-sort></th>
            </tr>
          </thead>
          <tbody>
            @for (g of rows(); track g.meta.name) {
              <tr class="mp-row-link" (click)="open(g.meta.name)">
                <td><a class="mp-link" [routerLink]="['/grants', g.meta.name]">{{ g.meta.name }}</a></td>
                <td>
                  <a class="mp-link" [routerLink]="['/subjects/resolve']" [queryParams]="{ subject: g.spec.subject }" (click)="$event.stopPropagation()">{{ g.spec.subject }}</a>
                </td>
                <td>
                  @for (c of g.spec.capabilities; track c; let last = $last) {
                    @if (lists.capability(c)) {
                      <a class="mp-link text-[12.5px]" [routerLink]="['/capabilities', c]" (click)="$event.stopPropagation()">{{ c }}</a>
                    } @else {
                      <span class="font-mono text-[12.5px] text-error line-through" title="not found">{{ c }}</span>
                    }@if (!last) {, }
                  }
                </td>
                <td><mp-condition label="Valid" bare [condition]="get(g.status.conditions, 'Valid')" /></td>
                <td><mp-condition label="Ready" bare [condition]="get(g.status.conditions, 'Ready')" /></td>
              </tr>
            } @empty {
              <tr><td colspan="5" class="text-fg-subtle">{{ lists.grantItems().length ? 'No grant matches.' : 'No MCPGrant exists.' }}</td></tr>
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
