import { afterNextRender, DestroyRef, Directive, ElementRef, inject, output } from '@angular/core';

/**
 * Emits when its element scrolls into view: put it after the last row of a
 * list that loads more as the reader reaches its end.
 */
@Directive({ selector: '[appNearEnd]' })
export class NearEnd {
  readonly appNearEnd = output<void>();

  constructor() {
    const element = inject<ElementRef<HTMLElement>>(ElementRef).nativeElement;
    const destroy = inject(DestroyRef);
    afterNextRender(() => {
      if (typeof IntersectionObserver === 'undefined') {
        return;
      }
      // A margin below the viewport, so the next rows are there before the
      // reader gets to them.
      const observer = new IntersectionObserver((entries) => {
        if (entries.some((e) => e.isIntersecting)) {
          this.appNearEnd.emit();
        }
      }, { rootMargin: '0px 0px 400px 0px' });
      observer.observe(element);
      destroy.onDestroy(() => observer.disconnect());
    });
  }
}
