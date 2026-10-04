import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { ActivatedRoute } from '@angular/router';
import { mpQueryParams } from '@metapatrol/ng';
import { renderMarkdown } from '@metapatrol/ng/markdown';
import { map } from 'rxjs';

/** Instructions, rendered as the Markdown they usually are, or raw. */
@Component({
  selector: 'app-instructions',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <section class="mp-panel mp-panel-flush">
      <header class="mp-panel-head">
        <h2 class="mp-label">Instructions</h2>
        @if (text()) {
          <div class="inline-flex overflow-hidden rounded-md border border-line text-xs" role="group" aria-label="Show as">
            @for (m of modes; track m) {
              <button
                type="button"
                class="px-2.5 py-1 capitalize not-first:border-l not-first:border-line"
                [class]="mode() === m ? 'bg-signal-wash font-semibold text-signal-fg' : 'text-fg-muted'"
                [attr.aria-pressed]="mode() === m"
                (click)="setMode(m)"
              >
                {{ m }}
              </button>
            }
          </div>
        }
      </header>
      @if (!text()) {
        <p class="text-fg-subtle px-4 py-3 text-sm">No instructions.</p>
      } @else if (mode() === 'rendered') {
        <div class="mp-markdown px-4 py-3" [innerHTML]="html()"></div>
      } @else {
        <pre class="overflow-x-auto px-4 py-3 font-mono text-[12.5px] leading-relaxed whitespace-pre-wrap">{{ text() }}</pre>
      }
    </section>
  `,
})
export class InstructionsPanel {
  readonly text = input<string | undefined>();
  protected readonly modes = ['rendered', 'raw'] as const;
  /** ?instructions=raw, so the raw text can be linked to. */
  private readonly param = toSignal(inject(ActivatedRoute).queryParamMap.pipe(map((q) => q.get('instructions'))));
  protected readonly mode = computed(() => (this.param() === 'raw' ? 'raw' : 'rendered'));
  private readonly setQuery = mpQueryParams();

  protected setMode(mode: 'rendered' | 'raw'): void {
    this.setQuery({ instructions: mode === 'raw' ? 'raw' : null }, { replace: true });
  }
  protected readonly html = computed(() => renderMarkdown(this.text() ?? ''));
}
