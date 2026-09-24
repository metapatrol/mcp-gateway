import { authRows } from './auth';

describe('authRows', () => {
  it('shows Secrets by name and key', () => {
    const rows = authRows({
      type: 'ClientCredentials',
      clientCredentials: {
        tokenURL: 'https://idp/token',
        clientIDRef: { name: 'search', key: 'CLIENT_ID' },
        clientSecretRef: { name: 'search', key: 'CLIENT_SECRET' },
        scopes: ['search.read'],
      },
    });
    expect(rows).toEqual([
      ['type', 'ClientCredentials'],
      ['token URL', 'https://idp/token'],
      ['client ID', 'Secret search, key CLIENT_ID'],
      ['client secret', 'Secret search, key CLIENT_SECRET'],
      ['scopes', 'search.read'],
    ]);
  });

  it('marks what Passthrough uses for discovery', () => {
    expect(authRows({ type: 'Passthrough' })).toEqual([
      ['type', 'Passthrough'],
      ['discovery', 'without credentials'],
    ]);
    const rows = authRows({ type: 'Passthrough', passthrough: { discovery: { apiKey: { secretRef: { name: 't', key: 'token' } } } } });
    expect(rows).toContainEqual(['discovery · key', 'Secret t, key token']);
  });
});
