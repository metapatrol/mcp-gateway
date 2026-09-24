import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { Inspect } from '../core/inspect';

export type SourceKind = 'spec' | 'status' | 'gateway';

const LOOK: Record<SourceKind, { text: string; title: string; classes: string }> = {
  spec: {
    text: 'spec',
    title: 'As written in the manifest',
    classes: 'border border-slate-200 bg-slate-50 text-slate-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-400',
  },
  status: {
    text: 'status',
    title: 'As the controller last reported it',
    classes: 'bg-accent-50 text-accent-700 dark:bg-accent-950 dark:text-accent-300',
  },
  gateway: {
    text: 'gateway',
    title: 'Live from the gateway replica that answered: its catalogs and its resolution',
    classes: 'bg-teal-50 text-teal-700 dark:bg-teal-950 dark:text-teal-300',
  },
};

/** Marks where a piece of data comes from. Hidden when the viewer turns sources off. */
@Component({
  selector: 'app-src',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (inspect.showSources()) {
      <span
        class="ml-1.5 inline-block rounded px-1 py-0.5 align-[1px] font-mono text-[10px] leading-none font-medium tracking-normal normal-case"
        [class]="look().classes"
        [title]="look().title"
        >{{ look().text }}</span
      >
    }
  `,
})
export class SrcTag {
  protected readonly inspect = inject(Inspect);
  readonly kind = input.required<SourceKind>();
  protected readonly look = computed(() => LOOK[this.kind()]);
}
