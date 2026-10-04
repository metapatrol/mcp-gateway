import { provideHttpClient, withFetch } from '@angular/common/http';
import { ApplicationConfig, provideBrowserGlobalErrorListeners } from '@angular/core';
import { provideRouter, withComponentInputBinding } from '@angular/router';
import { provideMetapatrol, provideMpIcons } from '@metapatrol/ng';
import { routes } from './app.routes';
import { INHERITED } from './core/conditions';
import { ICONS } from './shared/icons';

/** The theme choice was kept as mcp-gateway-ui.theme before @metapatrol/ng; carry it over once. */
function adoptThemeChoice(): void {
  try {
    const old = localStorage.getItem('mcp-gateway-ui.theme');
    if (old !== null) {
      if (localStorage.getItem('mcp-gateway.theme') === null) {
        localStorage.setItem('mcp-gateway.theme', old);
      }
      localStorage.removeItem('mcp-gateway-ui.theme');
    }
  } catch {
    // no storage, nothing to carry over
  }
}
adoptThemeChoice();

export const appConfig: ApplicationConfig = {
  providers: [
    provideBrowserGlobalErrorListeners(),
    // the sidebar and the theme are remembered as mcp-gateway.sidebar.collapsed and mcp-gateway.theme
    provideMetapatrol({
      storagePrefix: 'mcp-gateway',
      serviceName: 'the gateway',
      inheritedReasons: INHERITED,
    }),
    provideMpIcons(ICONS),
    provideRouter(routes, withComponentInputBinding()),
    provideHttpClient(withFetch()),
  ],
};
