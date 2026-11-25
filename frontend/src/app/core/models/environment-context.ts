import type { VariableNameRule } from '../../../../bindings/snap-rq/backend/models';

export interface EnvironmentVariableValue {
  key: string;
  value: string;
  /** Secret values stay in the backend: `{{key}}` is left as it is here. */
  secret?: boolean;
}

// The backend's rule (backend/models/variable_name.go), set once at startup.
// Until then no name is valid and no placeholder matches.
let rule: VariableNameRule | null = null;
let validName = /(?!)/;
let placeholder = '(?!)';

export function setVariableNameRule(next: VariableNameRule): void {
  rule = next;
  validName = new RegExp(`^[${next.chars}]+$`);
  placeholder = String.raw`\{\{\s*([${next.chars}]+)\s*\}\}`;
}

export function variableNameError(): string {
  return rule?.message ?? '';
}

export function isValidVariableName(name: string): boolean {
  return validName.test(name);
}

/** `{{name}}` with optional padding; group 1 is the name. A new global regex per call, so no lastIndex is shared. */
export function variablePlaceholderRegex(): RegExp {
  return new RegExp(placeholder, 'g');
}

/** The same placeholder inside JSON string quotes. */
export function quotedVariablePlaceholderRegex(): RegExp {
  return new RegExp(`"${placeholder}"`, 'g');
}

/**
 * An environment resolved into an interpolation context.
 *
 * For display only (the request list's URL column), which can't wait for a
 * backend round trip. What a request sends is filled in by the backend
 * (backend/environment).
 *
 * Secret variables are skipped: the frontend never has their values, so
 * `{{key}}` stays visible.
 */
export class EnvironmentContext {
  private readonly values: ReadonlyMap<string, string>;

  constructor(
    readonly id: number | null,
    readonly name: string,
    variables: readonly EnvironmentVariableValue[],
  ) {
    const values = new Map<string, string>();
    for (const variable of variables) {
      if (variable.secret) continue;
      // Last definition wins, matching the previous backend map semantics.
      values.set(variable.key, variable.value);
    }
    this.values = values;
  }

  static empty(): EnvironmentContext {
    return new EnvironmentContext(null, '', []);
  }

  /** Resolves `{{ key }}` placeholders; unknown keys are left untouched. */
  interpolate(text: string): string {
    if (this.values.size === 0 || !text.includes('{{')) return text;
    return text.replace(variablePlaceholderRegex(), (match, key: string) => {
      const value = this.values.get(key);
      return value === undefined ? match : value;
    });
  }
}
