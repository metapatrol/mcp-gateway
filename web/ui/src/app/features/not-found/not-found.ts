import { ChangeDetectionStrategy, Component } from '@angular/core';
import { RouterLink } from '@angular/router';

@Component({
  selector: 'app-not-found',
  imports: [RouterLink],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <h1 class="text-2xl font-semibold">Nothing here</h1>
    <p class="text-fg-muted mt-2">This address is not a page of the console. <a routerLink="/overview" class="text-signal-fg hover:underline">Go to the overview</a>.</p>
  `,
})
export class NotFound {}
