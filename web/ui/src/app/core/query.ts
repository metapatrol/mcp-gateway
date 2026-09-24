import { inject } from '@angular/core';
import { ActivatedRoute, Params, Router } from '@angular/router';

/**
 * Sets query parameters of the current page, keeping the others, so what a
 * page shows can be linked to and survives a reload. A null value removes the
 * parameter; defaults are left out of the URL that way.
 *
 * Call it in an injection context; the returned function sets them.
 */
export function queryParams(): (params: Params, opts?: { replace?: boolean }) => void {
  const router = inject(Router);
  const route = inject(ActivatedRoute);
  return (params, opts) =>
    void router.navigate([], {
      relativeTo: route,
      queryParams: params,
      queryParamsHandling: 'merge',
      replaceUrl: opts?.replace ?? false,
    });
}
