import { ChangeDetectionStrategy, Component, input } from '@angular/core';
import { RouterLink } from '@angular/router';

/** "Tool sets / memory-engram": where the page is, and what it is, in one line. */
@Component({
  selector: 'app-detail-title',
  imports: [RouterLink],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <h1 class="flex min-w-0 flex-wrap items-baseline gap-x-2 text-xl font-semibold tracking-tight">
      <a [routerLink]="link()" class="text-[15px] font-normal text-muted hover:text-accent">{{ section() }}</a>
      <span class="text-[15px] font-normal text-faint" aria-hidden="true">/</span>
      <span class="min-w-0 font-mono [overflow-wrap:anywhere]">{{ name() }}</span>
    </h1>
  `,
})
export class DetailTitle {
  readonly section = input.required<string>();
  readonly link = input.required<string>();
  readonly name = input.required<string>();
}
