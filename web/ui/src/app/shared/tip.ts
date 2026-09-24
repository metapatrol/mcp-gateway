import { DOCUMENT } from '@angular/common';
import { DestroyRef, Directive, ElementRef, inject, input } from '@angular/core';

let nextId = 0;

/**
 * A tooltip for what is behind an element: shown on hover and on keyboard
 * focus, after a moment, in the console's colours. It is placed on the page
 * rather than inside the element, so a table's scroll container does not
 * clip it. An empty text shows nothing.
 */
@Directive({
  selector: '[appTip]',
  host: {
    '(mouseenter)': 'schedule()',
    '(focusin)': 'schedule()',
    '(mouseleave)': 'hide()',
    '(focusout)': 'hide()',
    '[attr.tabindex]': 'focusable() ? 0 : null',
    '[attr.aria-describedby]': 'shown ? id : null',
  },
})
export class Tip {
  readonly appTip = input<string | null | undefined>();
  /** Make the element reachable by keyboard, for elements that are not already. */
  readonly focusable = input(false);

  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly document = inject(DOCUMENT);
  protected readonly id = `tip-${nextId++}`;
  protected shown = false;
  private timer?: ReturnType<typeof setTimeout>;
  private el?: HTMLElement;

  constructor() {
    inject(DestroyRef).onDestroy(() => this.hide());
  }

  protected schedule(): void {
    const text = this.appTip();
    if (!text) {
      return;
    }
    clearTimeout(this.timer);
    this.timer = setTimeout(() => this.show(text), 250);
  }

  protected hide(): void {
    clearTimeout(this.timer);
    this.document.defaultView?.removeEventListener('scroll', this.onScroll, { capture: true });
    this.el?.remove();
    this.el = undefined;
    this.shown = false;
  }

  private show(text: string): void {
    this.hide();
    const el = this.document.createElement('div');
    el.id = this.id;
    el.setAttribute('role', 'tooltip');
    el.textContent = text;
    el.className =
      'pointer-events-none fixed z-50 max-w-sm rounded-md border border-line-strong bg-surface px-2.5 py-1.5 text-xs leading-snug text-ink shadow-lg whitespace-pre-line';
    this.document.body.appendChild(el);

    const anchor = this.host.nativeElement.getBoundingClientRect();
    const tip = el.getBoundingClientRect();
    const view = this.document.defaultView;
    const width = view?.innerWidth ?? 1024;
    const gap = 6;
    // Above the element when there is room, below it otherwise; kept inside
    // the window sideways.
    const top = anchor.top - tip.height - gap >= 0 ? anchor.top - tip.height - gap : anchor.bottom + gap;
    const left = Math.min(Math.max(8, anchor.left + anchor.width / 2 - tip.width / 2), width - tip.width - 8);
    el.style.top = `${top}px`;
    el.style.left = `${left}px`;
    this.el = el;
    this.shown = true;
    // A scroll moves the element away from where the tooltip was placed.
    view?.addEventListener('scroll', this.onScroll, { once: true, capture: true });
  }

  private readonly onScroll = () => this.hide();
}
