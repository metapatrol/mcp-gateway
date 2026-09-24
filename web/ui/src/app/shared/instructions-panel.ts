import { ChangeDetectionStrategy, Component, input } from '@angular/core';

@Component({
  selector: 'app-instructions',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <section class="panel">
      <header class="panel-head"><h2 class="panel-title">Instructions</h2></header>
      @if (text()) {
        <pre class="overflow-x-auto px-4 py-3 font-mono text-[12.5px] leading-relaxed whitespace-pre-wrap">{{ text() }}</pre>
      } @else {
        <p class="faint px-4 py-3 text-sm">No instructions.</p>
      }
    </section>
  `,
})
export class InstructionsPanel {
  readonly text = input<string | undefined>();
}
