import { ChangeDetectionStrategy, Component, model } from '@angular/core';

export type DetailView = 'details' | 'mcp';

/** Tabs between a resource's page and what it comes to over MCP, as JSON. */
@Component({
  selector: 'app-view-switch',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="mb-4 flex gap-4 border-b border-line text-[13px]" role="tablist">
      @for (t of tabs; track t.key) {
        <button
          type="button"
          role="tab"
          class="-mb-px border-b-2 px-0.5 pb-2"
          [class]="view() === t.key ? 'border-accent font-semibold text-ink' : 'border-transparent text-muted hover:text-ink'"
          [attr.aria-selected]="view() === t.key"
          (click)="view.set(t.key)"
        >
          {{ t.label }}
        </button>
      }
    </div>
  `,
})
export class ViewSwitch {
  readonly view = model<DetailView>('details');
  protected readonly tabs: { key: DetailView; label: string }[] = [
    { key: 'details', label: 'Details' },
    { key: 'mcp', label: 'MCP' },
  ];
}
