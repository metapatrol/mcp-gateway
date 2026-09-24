import { DOCUMENT } from '@angular/common';
import { effect, inject, Injectable, signal } from '@angular/core';

export type Theme = 'light' | 'dark';

const THEME_KEY = 'mcp-gateway-ui.theme';

/**
 * Theme follows prefers-color-scheme until the viewer picks a theme. The
 * choice is a display preference, so it may outlive the tab.
 */
@Injectable({ providedIn: 'root' })
export class ThemeService {
  private readonly document = inject(DOCUMENT);
  private readonly chosen = signal<Theme | null>(this.readChoice());
  private readonly system = signal<Theme>(this.systemTheme());

  readonly theme = (): Theme => this.chosen() ?? this.system();

  constructor() {
    const media = this.document.defaultView?.matchMedia?.('(prefers-color-scheme: dark)');
    media?.addEventListener?.('change', (event) => this.system.set(event.matches ? 'dark' : 'light'));
    effect(() => {
      this.document.documentElement.classList.toggle('dark', this.theme() === 'dark');
    });
  }

  toggle(): void {
    const next: Theme = this.theme() === 'dark' ? 'light' : 'dark';
    this.chosen.set(next);
    try {
      this.document.defaultView?.localStorage.setItem(THEME_KEY, next);
    } catch {
      // Without storage the choice lasts for this page.
    }
  }

  private readChoice(): Theme | null {
    try {
      const value = this.document.defaultView?.localStorage.getItem(THEME_KEY);
      return value === 'dark' || value === 'light' ? value : null;
    } catch {
      return null;
    }
  }

  private systemTheme(): Theme {
    return this.document.defaultView?.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  }
}
