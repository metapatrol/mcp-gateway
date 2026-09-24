import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { listSubjects } from '../../api/functions';
import { SubjectSummary } from '../../api/models';
import { Inspect, live } from '../../core/inspect';
import { LoadState } from '../../shared/load-state';

/** Subjects whose name or grants contain the query, ignoring case. */
export function filterSubjects(items: SubjectSummary[], query: string): SubjectSummary[] {
  const q = query.trim().toLowerCase();
  if (!q) {
    return items;
  }
  return items.filter((s) => s.subject.toLowerCase().includes(q) || s.grants.some((g) => g.toLowerCase().includes(q)));
}

@Component({
  selector: 'app-subjects',
  imports: [RouterLink, LoadState],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <h1 class="mb-4 text-xl font-semibold tracking-tight">Subjects</h1>
    <app-load-state [error]="subjects.error()" [loading]="!subjects.hasValue()" what="subjects" />
    @if (subjects.hasValue()) {
      <section class="panel">
        <header class="panel-head">
          <form class="w-full max-w-md" (submit)="open($event)">
            <label for="subject" class="sr-only">Filter, or resolve any subject</label>
            <input
              id="subject"
              name="subject"
              class="input py-1"
              placeholder="Filter, or resolve any subject"
              [value]="query()"
              (input)="query.set($any($event.target).value)"
              autocomplete="off"
              spellcheck="false"
            />
          </form>
          <span class="faint text-xs">{{ rows().length }} of {{ all().length }}</span>
        </header>
        <div class="overflow-x-auto">
          <table class="data-table">
            <thead>
              <tr>
                <th>Subject</th>
                <th>Grants</th>
                <th class="text-right">Tools offered</th>
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
  protected readonly query = signal('');
  protected readonly all = computed(() => (this.subjects.hasValue() ? (this.subjects.value()?.items ?? []) : []));
  protected readonly rows = computed(() => filterSubjects(this.all(), this.query()));
  /** The typed text, when it is not a subject any grant names: it can still be resolved. */
  protected readonly unknown = computed(() => {
    const q = this.query().trim();
    return q && !this.all().some((s) => s.subject === q) ? q : '';
  });

  protected go(subject: string): void {
    void this.router.navigate(['/subjects/resolve'], { queryParams: { subject } });
  }

  /** Enter opens the one match, an exact match, or else resolves what was typed. */
  protected open(event: Event): void {
    event.preventDefault();
    const q = this.query().trim();
    if (!q) {
      return;
    }
    const rows = this.rows();
    const exact = rows.find((s) => s.subject === q);
    this.go(exact?.subject ?? (rows.length === 1 ? rows[0].subject : q));
  }
}
