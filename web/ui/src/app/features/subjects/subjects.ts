import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { listSubjects } from '../../api/functions';
import { SubjectSummary } from '../../api/models';
import { Inspect, live } from '../../core/inspect';
import { parseSort, searchRows, sortRows } from '../../core/table';
import { ListSearch } from '../../shared/list-search';
import { SortHeader } from '../../shared/sort-header';
import { LoadState } from '../../shared/load-state';

/** Subjects whose name or grants contain the query, ignoring case. */
export function filterSubjects(items: SubjectSummary[], query: string): SubjectSummary[] {
  return searchRows(items, query, (s) => [s.subject, ...s.grants]);
}

@Component({
  selector: 'app-subjects',
  imports: [RouterLink, LoadState, ListSearch, SortHeader],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <h1 class="mb-4 text-xl font-semibold tracking-tight">Subjects</h1>
    <app-load-state [error]="subjects.error()" [loading]="!subjects.hasValue()" what="subjects" />
    @if (subjects.hasValue()) {
      <section class="panel">
        <header class="panel-head">
          <app-list-search
            placeholder="Filter, or resolve any subject"
            [query]="query()"
            [shown]="rows().length"
            [total]="all().length"
            (submitted)="open()"
          />
        </header>
        <div class="overflow-x-auto">
          <table class="data-table">
            <thead>
              <tr>
                <th><app-sort key="name" [sort]="sort()">Subject</app-sort></th>
                <th><app-sort key="grants" [sort]="sort()">Grants</app-sort></th>
                <th class="text-right"><app-sort key="tools" [sort]="sort()">Tools offered</app-sort></th>
              </tr>
            </thead>
            <tbody>
              @for (s of rows(); track s.subject) {
                <tr class="row-link" (click)="go(s.subject)">
                  <td>
                    <a class="link" [routerLink]="['/subjects/resolve']" [queryParams]="{ subject: s.subject }">{{ s.subject }}</a>
                  </td>
                  <td>
                    @for (g of s.grants; track g; let last = $last) {
                      <a class="link text-[12.5px]" [routerLink]="['/grants', g]" (click)="$event.stopPropagation()">{{ g }}</a>@if (!last) {, }
                    }
                  </td>
                  <td class="num">{{ s.toolCount }}</td>
                </tr>
              }
              @if (unknown(); as q) {
                <tr class="row-link" (click)="go(q)">
                  <td colspan="3">
                    <a class="link" [routerLink]="['/subjects/resolve']" [queryParams]="{ subject: q }">Resolve “{{ q }}”</a>
                    <span class="faint ml-2 text-xs">no grant names it</span>
                  </td>
                </tr>
              } @else if (!rows().length) {
                <tr><td colspan="3" class="faint">No grant names a subject.</td></tr>
              }
            </tbody>
          </table>
        </div>
      </section>
    }
  `,
})
export class Subjects {
  private readonly inspect = inject(Inspect);
  private readonly router = inject(Router);
  protected readonly subjects = live(
    () => ({}),
    () => this.inspect.call(listSubjects, {}),
  );
  /** ?q= and ?sort=. */
  readonly q = input<string>();
  readonly sort = input<string>();
  protected readonly query = computed(() => this.q() ?? '');
  protected readonly all = computed(() => (this.subjects.hasValue() ? (this.subjects.value()?.items ?? []) : []));
  protected readonly rows = computed(() =>
    sortRows(
      filterSubjects(this.all(), this.query()),
      parseSort(this.sort()),
      { name: (s) => s.subject, grants: (s) => s.grants.join(', '), tools: (s) => s.toolCount },
      (s) => s.subject,
    ),
  );
  /** The typed text, when it is not a subject any grant names: it can still be resolved. */
  protected readonly unknown = computed(() => {
    const q = this.query().trim();
    return q && !this.all().some((s) => s.subject === q) ? q : '';
  });

  protected go(subject: string): void {
    void this.router.navigate(['/subjects/resolve'], { queryParams: { subject } });
  }

  /** Enter opens the one match, an exact match, or else resolves what was typed. */
  protected open(): void {
    const q = this.query().trim();
    if (!q) {
      return;
    }
    const rows = this.rows();
    const exact = rows.find((s) => s.subject === q);
    this.go(exact?.subject ?? (rows.length === 1 ? rows[0].subject : q));
  }
}
