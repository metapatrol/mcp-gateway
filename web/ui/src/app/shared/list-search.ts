import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';
import { queryParams } from '../core/query';

/**
 * The search field above a list, bound to ?q=, with how many rows it leaves.
 * Enter is passed on, for a page that has something to open.
 */
@Component({
  selector: 'app-list-search',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'contents' },
  template: `
    <form class="w-full max-w-md" role="search" (submit)="$event.preventDefault(); submitted.emit()">
      <label [for]="id" class="sr-only">{{ placeholder() }}</label>
      <input
        [id]="id"
        name="q"
        class="input py-1"
        [placeholder]="placeholder()"
        [value]="query()"
        (input)="search($any($event.target).value)"
        autocomplete="off"
        spellcheck="false"
      />
    </form>
    <span class="faint text-xs">{{ shown() }} of {{ total() }}</span>
  `,
})
export class ListSearch {
  readonly query = input('');
  readonly shown = input(0);
  readonly total = input(0);
  readonly placeholder = input('Filter');
  readonly submitted = output();
  protected readonly id = 'list-search';
  private readonly setQuery = queryParams();

  /** Typing replaces the history entry rather than adding one per key. */
  protected search(value: string): void {
    this.setQuery({ q: value || null }, { replace: true });
  }
}
