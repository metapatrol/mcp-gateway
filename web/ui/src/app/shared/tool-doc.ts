import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { ToolDefinition } from '../api/models';
import { parameters } from '../core/schema';

/**
 * A tool's description and arguments: the text an agent decides by. It is
 * shown as the backend sent it; the gateway passes it on unchanged.
 */
@Component({
  selector: 'app-tool-doc',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (tool().description; as d) {
      <p class="mt-1 max-w-[72ch] font-sans text-[12.5px] leading-snug whitespace-pre-line text-slate-600 dark:text-slate-400">{{ d }}</p>
    } @else {
      <p class="faint mt-1 font-sans text-[12.5px]">No description.</p>
    }
    @if (hints().length) {
      <div class="mt-1 flex flex-wrap gap-1 font-sans">
        @for (h of hints(); track h.text) {
          <span class="rounded px-1.5 py-0.5 text-[10.5px] font-medium" [class]="h.classes" [title]="'A hint from the backend, not verified by the gateway'">{{ h.text }}</span>
        }
      </div>
    }
    @if (params().length) {
      <details class="mt-1 font-sans text-xs" [open]="open()">
        <summary class="w-max cursor-pointer text-accent-600 dark:text-accent-300">
          input · {{ params().length }} {{ params().length === 1 ? 'parameter' : 'parameters' }}
        </summary>
        <ul class="mt-1.5 flex flex-col gap-1 border-l-2 border-slate-200 pl-2.5 dark:border-slate-700">
          @for (p of params(); track p.name) {
            <li class="flex flex-wrap items-baseline gap-x-2">
              <code class="text-slate-900 dark:text-slate-100">{{ p.name }}</code>
              <span class="font-mono text-[11.5px] text-teal-700 dark:text-teal-300">{{ p.type }}</span>
              @if (p.required) {
                <span class="text-[10px] font-semibold tracking-wide text-amber-700 uppercase dark:text-amber-400">required</span>
              }
              @if (p.description) {
                <span class="faint">{{ p.description }}</span>
              }
            </li>
          }
        </ul>
      </details>
    }
  `,
})
export class ToolDoc {
  readonly tool = input.required<ToolDefinition>();
  readonly open = input(false);
  protected readonly params = computed(() => parameters(this.tool().inputSchema));
  protected readonly hints = computed(() => {
    const a = this.tool().annotations;
    const out: { text: string; classes: string }[] = [];
    if (a?.readOnlyHint) {
      out.push({ text: 'read-only', classes: 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/60 dark:text-emerald-300' });
    }
    if (a?.destructiveHint && !a?.readOnlyHint) {
      out.push({ text: 'destructive', classes: 'bg-red-50 text-red-700 dark:bg-red-950/60 dark:text-red-300' });
    }
    if (a?.idempotentHint) {
      out.push({ text: 'idempotent', classes: 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300' });
    }
    if (a?.openWorldHint) {
      out.push({ text: 'open world', classes: 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300' });
    }
    return out;
  });
}
