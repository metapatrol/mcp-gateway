import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { conditionRank, MpCondition, MpListSearch, MpLoadState, MpPageHeader, MpSort, parseSort, searchRows, sortRows } from '@metapatrol/ng';
import { Condition } from '../../api/models';
import { condition } from '../../core/conditions';
import { Lists } from '../../core/lists';


@Component({
  selector: 'app-toolsets',
  imports: [MpPageHeader, RouterLink, MpCondition, MpLoadState, MpListSearch, MpSort],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <mp-page-header title="Tool sets" note="MCPToolSet" />
    <mp-load-state [error]="lists.toolSets.error()" [loading]="!lists.toolSets.hasValue()" what="tool sets" />
    @if (lists.toolSets.hasValue()) {
      <section class="mp-panel mp-panel-flush">
        <header class="mp-panel-head">
          <mp-list-search placeholder="Filter by name, server or tool" [query]="q() ?? ''" [shown]="rows().length" [total]="lists.toolSetItems().length" />
        </header>
        <div class="overflow-x-auto">
        <table class="mp-table">
          <thead>
            <tr>
              <th><mp-sort key="name" [sort]="sort()">Name</mp-sort></th>
              <th><mp-sort key="server" [sort]="sort()">Server</mp-sort></th>
              <th><mp-sort key="selection" [sort]="sort()">Selection</mp-sort></th>
              <th class="text-right"><mp-sort key="tools" [sort]="sort()">Tools</mp-sort></th>
              <th><mp-sort key="valid" [sort]="sort()">Valid</mp-sort></th>
              <th><mp-sort key="ready" [sort]="sort()">Ready</mp-sort></th>
            </tr>
          </thead>
          <tbody>
            @for (t of rows(); track t.meta.name) {
              <tr class="mp-row-link" (click)="open(t.meta.name)">
                <td><a class="mp-link" [routerLink]="['/toolsets', t.meta.name]">{{ t.meta.name }}</a></td>
                <td>
                  <a class="mp-link text-[12.5px]" [routerLink]="['/servers', t.spec.serverRef]" (click)="$event.stopPropagation()">{{ t.spec.serverRef }}</a>
                </td>
                <td class="text-fg-muted font-mono text-[12.5px]">
                  {{ t.spec.tools.join(', ') }}
                  @if (t.spec.exclude?.length) {
                    <span class="text-fg-subtle"> − {{ t.spec.exclude.join(', ') }}</span>
                  }
                  @if (t.spec.prefix) {
                    <span class="text-fg-subtle"> · prefix {{ t.spec.prefix }}</span>
                  }
                </td>
                <td class="mp-num">{{ t.status.resolvedToolCount }}</td>
                <td><mp-condition label="Valid" bare [condition]="get(t.status.conditions, 'Valid')" /></td>
                <td><mp-condition label="Ready" bare [condition]="get(t.status.conditions, 'Ready')" /></td>
              </tr>
            } @empty {
              <tr><td colspan="6" class="text-fg-subtle">{{ lists.toolSetItems().length ? 'No tool set matches.' : 'No MCPToolSet exists.' }}</td></tr>
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
