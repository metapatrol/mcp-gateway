import { BackendAuth, SecretKeyRef } from '../api/models';

const secret = (r: SecretKeyRef) => `Secret ${r.name}, key ${r.key}`;

/**
 * spec.auth as label/value rows. Secrets appear by name and key only: the
 * API never reads what is in them, and neither does the console.
 */
export function authRows(auth: BackendAuth): [string, string][] {
  const rows: [string, string][] = [['type', auth.type]];
  const cc = auth.clientCredentials ?? auth.passthrough?.discovery?.clientCredentials;
  const key = auth.apiKey ?? auth.passthrough?.discovery?.apiKey;
  const via = auth.passthrough?.discovery ? 'discovery · ' : '';
  if (auth.type === 'Passthrough' && !auth.passthrough?.discovery) {
    rows.push(['discovery', 'without credentials']);
  }
  if (cc) {
    rows.push([`${via}token URL`, cc.tokenURL]);
    rows.push([`${via}client ID`, cc.clientID ?? (cc.clientIDRef ? secret(cc.clientIDRef) : '—')]);
    rows.push([`${via}client secret`, secret(cc.clientSecretRef)]);
    if (cc.scopes?.length) {
      rows.push([`${via}scopes`, cc.scopes.join(' ')]);
    }
    if (cc.audience?.length) {
      rows.push([`${via}audience`, cc.audience.join(' ')]);
    }
  }
  if (key) {
    rows.push([`${via}key`, secret(key.secretRef)]);
    rows.push([`${via}header`, key.header ?? 'Authorization']);
    if (key.prefix !== undefined && key.prefix !== null) {
      rows.push([`${via}prefix`, key.prefix === '' ? '(none)' : JSON.stringify(key.prefix)]);
    }
  }
  return rows;
}
