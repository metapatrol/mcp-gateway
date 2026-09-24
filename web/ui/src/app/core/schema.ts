/** One argument of a tool, read from its input schema. */
export interface Param {
  name: string;
  type: string;
  required: boolean;
  description?: string;
}

type Schema = Record<string, unknown>;

function isSchema(v: unknown): v is Schema {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

/**
 * A short type for display: what a person needs to tell arguments apart, not
 * a full rendering of JSON Schema.
 */
export function typeOf(schema: unknown): string {
  if (!isSchema(schema)) {
    return 'any';
  }
  if ('const' in schema) {
    return JSON.stringify(schema['const']);
  }
  if (Array.isArray(schema['enum'])) {
    return schema['enum'].map((v) => JSON.stringify(v)).join(' | ');
  }
  for (const key of ['oneOf', 'anyOf'] as const) {
    const options = schema[key];
    if (Array.isArray(options)) {
      return options.map(typeOf).join(' | ');
    }
  }
  if (typeof schema['$ref'] === 'string') {
    return schema['$ref'].split('/').pop() ?? 'object';
  }
  const type = schema['type'];
  if (Array.isArray(type)) {
    return type.join(' | ');
  }
  if (type === 'array') {
    return `${typeOf(schema['items'])}[]`;
  }
  if (typeof type === 'string') {
    return type;
  }
  return isSchema(schema['properties']) ? 'object' : 'any';
}

/** The top-level arguments of an input schema, required ones first. */
export function parameters(schema: unknown): Param[] {
  if (!isSchema(schema) || !isSchema(schema['properties'])) {
    return [];
  }
  const required = new Set(Array.isArray(schema['required']) ? (schema['required'] as string[]) : []);
  const out = Object.entries(schema['properties']).map(([name, prop]) => ({
    name,
    type: typeOf(prop),
    required: required.has(name),
    description: isSchema(prop) && typeof prop['description'] === 'string' ? prop['description'] : undefined,
  }));
  return out.sort((a, b) => Number(b.required) - Number(a.required));
}
