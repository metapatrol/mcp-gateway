import { ChangeDetectionStrategy, Component, computed, effect, ElementRef, inject, input } from '@angular/core';
import { queryParams } from '../core/query';
import { formatSort, nextSort, parseSort } from '../core/table';

/**
 * The content of a column header that sorts its table by that column, through
 * ?sort=. A second click turns the order around. It marks the header cell it
 * sits in with aria-sort.
 */
@Component({
  selector: 'app-sort',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <button
      type="button"
      class="inline-flex items-center gap-1 uppercase tracking-[inherit] hover:text-ink"
      [class.text-ink]="active()"
      (click)="toggle()"
    >
      <ng-content />
      <span class="w-2 text-[10px]" aria-hidden="true">{{ active() ? (current().desc ? '↓' : '↑') : '' }}</span>
    </button>
  `,
})
export class SortHeader {
  /** The column's key in ?sort=. */
  readonly key = input.required<string>();
  /** The page's ?sort=. */
  readonly sort = input<string>();

  protected readonly current = computed(() => parseSort(this.sort()));
  protected readonly active = computed(() => this.current().key === this.key());
  protected readonly ariaSort = computed(() => (this.active() ? (this.current().desc ? 'descending' : 'ascending') : null));
  private readonly setQuery = queryParams();

  constructor() {
    const cell = inject<ElementRef<HTMLElement>>(ElementRef).nativeElement;
    effect(() => {
      const th = cell.closest('th');
      const value = this.ariaSort();
      if (value) {
        th?.setAttribute('aria-sort', value);
      } else {
        th?.removeAttribute('aria-sort');
      }
    });
  }

  protected toggle(): void {
    this.setQuery({ sort: formatSort(nextSort(this.current(), this.key())) }, { replace: true });
  }
}
