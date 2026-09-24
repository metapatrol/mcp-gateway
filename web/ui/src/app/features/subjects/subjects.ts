import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { listSubjects } from '../../api/functions';
import { Inspect, live } from '../../core/inspect';
import { LoadState } from '../../shared/load-state';

@Component({
  selector: 'app-subjects',
  imports: [RouterLink, LoadState],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <header class="mb-5 flex flex-wrap items-end justify-between gap-4">
      <h1 class="text-2xl font-semibold tracking-tight">Subjects</h1>
      <form class="flex w-full max-w-md gap-2" (submit)="open($event)">
        <label for="subject" class="sr-only">Subject</label>
        <input
          id="subject"
          name="subject"
          class="input"
          placeholder="any subject"
          [value]="typed()"
          (input)="typed.set($any($event.target).value)"
          autocomplete="off"
          spellcheck="false"
        />
        <button type="submit" class="btn shrink-0" [disabled]="!typed().trim()">Resolve</button>
      </form>
    </header>
    <app-load-state [error]="subjects.error()" [loading]="!subjects.hasValue()" what="subjects" />
    @if (subjects.hasValue()) {
      <section class="panel overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th>Subject</th>
              <th>Grants</th>
              <th class="text-right">Tools offered</th>
            </tr>
          </thead>
          <tbody>
            @for (s of subjects.value().items; track s.subject) {
              <tr class="row-link" (click)="go(s.subject)">
                <td>
                  <a class="link" [routerLink]="['/subjects/resolve']" [queryParams]="{ subject: s.subject }">{{ s.subject }}</a>
                </td>
                <td class="font-mono text-[12.5px]">
                  @for (g of s.grants; track g; let last = $last) {
                    <a class="link text-[12.5px]" [routerLink]="['/grants', g]" (click)="$event.stopPropagation()">{{ g }}</a>@if (!last) {, }
                  }
                </td>
                <td class="num">{{ s.toolCount }}</td>
              </tr>
            } @empty {
              <tr><td colspan="3" class="faint">No grant names a subject.</td></tr>
            }
          </tbody>
        </table>
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
  protected readonly typed = signal('');

  protected go(subject: string): void {
    void this.router.navigate(['/subjects/resolve'], { queryParams: { subject } });
  }

  protected open(event: Event): void {
    event.preventDefault();
    const subject = this.typed().trim();
    if (subject) {
      this.go(subject);
    }
  }
}
