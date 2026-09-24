import { parameters, typeOf } from './schema';

describe('typeOf', () => {
  it('reads the shapes tool schemas use', () => {
    expect(typeOf({ type: 'string' })).toBe('string');
    expect(typeOf({ type: ['string', 'null'] })).toBe('string | null');
    expect(typeOf({ type: 'array', items: { type: 'integer' } })).toBe('integer[]');
    expect(typeOf({ enum: ['symbols', 'text'] })).toBe('"symbols" | "text"');
    expect(typeOf({ const: 42 })).toBe('42');
    expect(typeOf({ anyOf: [{ type: 'string' }, { type: 'number' }] })).toBe('string | number');
    expect(typeOf({ $ref: '#/$defs/Location' })).toBe('Location');
    expect(typeOf({ properties: {} })).toBe('object');
    expect(typeOf(undefined)).toBe('any');
  });
});

describe('parameters', () => {
  it('lists top-level properties, required ones first', () => {
    const params = parameters({
      type: 'object',
      properties: {
        limit: { type: 'integer', description: 'default 50' },
        query: { type: 'string', description: 'term to look for' },
      },
      required: ['query'],
    });
    expect(params).toEqual([
      { name: 'query', type: 'string', required: true, description: 'term to look for' },
      { name: 'limit', type: 'integer', required: false, description: 'default 50' },
    ]);
  });

  it('is empty for a schema without properties', () => {
    expect(parameters({ type: 'object' })).toEqual([]);
    expect(parameters(null)).toEqual([]);
  });
});
