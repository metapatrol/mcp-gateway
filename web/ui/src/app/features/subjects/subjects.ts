import { ChangeDetectionStrategy, Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { MpListSearch, MpLoadState, MpPageHeader, MpSort } from '@metapatrol/ng';
import { listSubjects } from '../../api/functions';
import { ListSubjects$Params } from '../../api/fn/subjects/list-subjects';
import { SubjectList } from '../../api/models';
import { Inspect, live } from '../../core/inspect';
import { NearEnd } from '../../shared/near-end';

/** Rows a page adds as the reader scrolls. */
export const PAGE = 100;
/** The most one answer of the inspect API carries. */
export const MAX_ROWS = 1000;

const SORTS = ['name', 'grants', 'tools'];

/** ?sort= as the API takes it: a column of this table, or the default. */
export function subjectSort(sort: string | undefined): ListSubjects$Params['sort'] {
  return sort && SORTS.includes(sort.replace(/^-/, '')) ? (sort as ListSubjects$Params['sort']) : undefined;
}

@Component({
  selector: 'app-subjects',
  imports: [MpPageHeader, RouterLink, MpLoadState, MpListSearch, MpSort, NearEnd],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <mp-page-header title="Subjects" />
    <mp-load-state [error]="subjects.error()" [loading]="!shown()" what="subjects" />
    @if (shown()) {
      <section class="mp-panel mp-panel-flush">
        <header class="mp-panel-head">
          <mp-list-search
            placeholder="Filter, or resolve any subject"
            [query]="query()"
            [shown]="rows().length"
            [total]="total()"
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
              @if (more() && !subjects.isLoading()) {
                <tr (appNearEnd)="showMore()"><td colspan="3"></td></tr>
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
  /** ?q= and ?sort=. */
  readonly q = input<string>();
  readonly sort = input<string>();
  protected readonly query = computed(() => this.q() ?? '');
  /**
   * How many rows to show. The API searches, sorts and cuts them, so the
   * gateway resolves only the subjects shown; a refresh reads as many again
   * as are shown, so the list does not jump back to its first page.
   */
  private readonly wanted = signal(PAGE);
  protected readonly subjects = live(
    () => ({ q: this.query() || undefined, sort: subjectSort(this.sort()), limit: this.wanted() }),
    (params) => this.inspect.call(listSubjects, params),
  );
  /**
   * The last answer, kept while the next one loads: a resource drops its
   * value when its parameters change, and the table would jump to the top
   * each time more rows are asked for.
   */
  protected readonly shown = signal<SubjectList | undefined>(undefined);
  protected readonly rows = computed(() => this.shown()?.items ?? []);
  protected readonly total = computed(() => this.shown()?.total ?? 0);
  /** Whether rows are left to load, as far as one answer can carry them. */
  protected readonly more = computed(() => this.rows().length < Math.min(this.total(), MAX_ROWS));

  constructor() {
    effect(() => {
      if (this.subjects.hasValue()) {
        this.shown.set(this.subjects.value());
      }
    });
    // A new search or order starts at the top.
    effect(() => {
      this.query();
      this.sort();
      untracked(() => this.wanted.set(PAGE));
    });
  }

  protected showMore(): void {
    this.wanted.set(Math.min(this.rows().length + PAGE, MAX_ROWS));
  }

  /** The typed text, when it is not a subject any grant names: it can still be resolved. */
  protected readonly unknown = computed(() => {
    const q = this.query().trim();
    return q && !this.rows().some((s) => s.subject === q) ? q : '';
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
