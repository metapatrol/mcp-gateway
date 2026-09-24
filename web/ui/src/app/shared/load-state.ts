import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { describeError, isNotFound } from '../core/inspect';

/**
 * What a page shows instead of its data: a quiet line while the first answer
 * is on its way, the API's own explanation when it fails.
 */
@Component({
  selector: 'app-load-state',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (error(); as err) {
      <div class="panel border-bad/40 bg-bad-soft px-4 py-3 text-sm text-bad" role="alert">
        @if (notFound()) {
          <p class="font-medium">No {{ what() }} named <code>{{ name() }}</code>.</p>
          <p class="mt-1 opacity-80">It may have been deleted, or the name is misspelt.</p>
        } @else {
          <p class="font-medium">The {{ what() }} could not be loaded.</p>
          <p class="mt-1 font-mono text-xs opacity-80">{{ message() }}</p>
        }
      </div>
    } @else if (loading()) {
      <p class="faint py-6 text-sm" aria-live="polite">Loading…</p>
    }
  `,
})
export class LoadState {
  readonly error = input<unknown>();
  readonly loading = input(false);
  readonly what = input('data');
  readonly name = input('');
  protected readonly notFound = computed(() => isNotFound(this.error()));
  protected readonly message = computed(() => describeError(this.error()));
}
