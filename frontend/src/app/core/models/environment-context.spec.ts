import { describe, expect, it } from 'vitest';
import cases from '../../../../../testdata/variable-names.json';
import {
  EnvironmentContext,
  isValidVariableName,
  quotedVariablePlaceholderRegex,
  variablePlaceholderRegex,
} from './environment-context';

// The same cases run against backend/models/variable_name.go, so a name the
// backend saves is always one the editor shows as a chip.

function matchedName(text: string): string | undefined {
  return variablePlaceholderRegex().exec(text)?.[1];
}

describe('variable names', () => {
  it.each(cases.valid)('accepts %j', (name) => {
    expect(isValidVariableName(name)).toBe(true);
  });

  it.each(cases.invalid)('rejects %j', (name) => {
    expect(isValidVariableName(name)).toBe(false);
  });

  it.each(cases.valid)('finds {{%s}} with and without padding', (name) => {
    expect(matchedName(`{{${name}}}`)).toBe(name);
    expect(matchedName(`{{ ${name} }}`)).toBe(name);
    expect(quotedVariablePlaceholderRegex().exec(`"{{${name}}}"`)?.[1]).toBe(name);
  });

  it.each(cases.invalid)('never finds %j as a name', (name) => {
    expect(matchedName(`{{${name}}}`)).not.toBe(name);
  });

  it('interpolates hyphenated and dotted names', () => {
    const env = new EnvironmentContext(1, 'Dev', [
      { key: 'fleet-management', value: 'fm' },
      { key: 'api.v2', value: 'v2' },
    ]);
    expect(env.interpolate('/{{ fleet-management }}/{{api.v2}}/{{missing}}')).toBe(
      '/fm/v2/{{missing}}',
    );
  });
});
