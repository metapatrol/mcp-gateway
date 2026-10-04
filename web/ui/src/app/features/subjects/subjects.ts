import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { MpListSearch, MpLoadState, MpSort, parseSort, searchRows, sortRows } from '@metapatrol/ng';
import { listSubjects } from '../../api/functions';
import { SubjectSummary } from '../../api/models';
import { Inspect, live } from '../../core/inspect';

/** Subjects whose name or grants contain the query, ignoring case. */
export function filterSubjects(items: SubjectSummary[], query: string): SubjectSummary[] {
  return searchRows(items, query, (s) => [s.subject, ...s.grants]);
}

@Component({
  selector: 'app-subjects',
  imports: [RouterLink, MpLoadState, MpListSearch, MpSort],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <h1 class="mb-4 text-xl font-semibold tracking-tight">Subjects</h1>
    <mp-load-state [error]="subjects.error()" [loading]="!subjects.hasValue()" what="subjects" />
    @if (subjects.hasValue()) {
      <section class="mp-panel mp-panel-flush">
        <header class="mp-panel-head">
          <mp-list-search
            placeholder="Filter, or resolve any subject"
            [query]="query()"
            [shown]="rows().length"
            [total]="all().length"
            (submitted)="open()"
          />
        </header>
        <div class="overflow-x-auto">
          <table class="mp-table">
            <thead>
              <tr>
                <th><mp-sort key="name" [sort]="sort()">Subject</mp-sort></th>
                <th><mp-sort key="grants" [sort]="sort()">Grants</mp-sort></th>
                <th class="text-right"><mp-sort key="tools" [sort]="sort()">Tools offered</mp-sort></th>
              </tr>
            </thead>
            <tbody>
              @for (s of rows(); track s.subject) {
                <tr class="mp-row-link" (click)="go(s.subject)">
                  <td>
                    <a class="mp-link" [routerLink]="['/subjects/resolve']" [queryParams]="{ subject: s.subject }">{{ s.subject }}</a>
                  </td>
                  <td>
                    @for (g of s.grants; track g; let last = $last) {
                      <a class="mp-link text-[12.5px]" [routerLink]="['/grants', g]" (click)="$event.stopPropagation()">{{ g }}</a>@if (!last) {, }
                    }
                  </td>
                  <td class="mp-num">{{ s.toolCount }}</td>
                </tr>
              }
              @if (unknown(); as q) {
                <tr class="mp-row-link" (click)="go(q)">
                  <td colspan="3">
                    <a class="mp-link" [routerLink]="['/subjects/resolve']" [queryParams]="{ subject: q }">Resolve “{{ q }}”</a>
                    <span class="text-fg-subtle ml-2 text-xs">no grant names it</span>
                  </td>
                </tr>
              } @else if (!rows().length) {
                <tr><td colspan="3" class="text-fg-subtle">No grant names a subject.</td></tr>
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
