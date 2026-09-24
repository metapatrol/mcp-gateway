import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';

/** What is wrong with the resource on this page, above everything else. */
@Component({
  selector: 'app-notice',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `<div class="rounded-[10px] border px-4 py-2.5 text-[13px]" [class]="classes()" role="status"><ng-content /></div>`,
})
export class Notice {
  readonly tone = input<'warn' | 'bad'>('warn');
  protected readonly classes = computed(() =>
    this.tone() === 'bad' ? 'border-bad/40 bg-bad-soft text-bad' : 'border-warn/40 bg-warn-soft text-warn',
  );
}
