export interface EnvironmentVariableValue {
  key: string;
  value: string;
  /** Secret values stay in the backend: `{{key}}` is left as it is here. */
  secret?: boolean;
}

// Mirrors backend/models/variable_name.go; testdata/variable-names.json tests both.
const VARIABLE_NAME_CHARS = 'A-Za-z0-9_.-';
const VARIABLE_NAME = new RegExp(`^[${VARIABLE_NAME_CHARS}]+$`);

export const VARIABLE_NAME_ERROR = 'Use only letters, digits, _, - and .';

export function isValidVariableName(name: string): boolean {
  return VARIABLE_NAME.test(name);
}

const PLACEHOLDER = String.raw`\{\{\s*([${VARIABLE_NAME_CHARS}]+)\s*\}\}`;

/** `{{name}}` with optional padding; group 1 is the name. A new global regex per call, so no lastIndex is shared. */
export function variablePlaceholderRegex(): RegExp {
  return new RegExp(PLACEHOLDER, 'g');
}

/** The same placeholder inside JSON string quotes. */
export function quotedVariablePlaceholderRegex(): RegExp {
  return new RegExp(`"${PLACEHOLDER}"`, 'g');
}

/**
 * An environment resolved into an interpolation context.
 *
 * This is the single implementation of `{{variable}}` interpolation in the
 * app: the frontend owns the environment's variables in memory (loaded on
 * environment selection and kept in sync by the variables editor), so display
 * and request execution both resolve through this entity.
 *
 * Secret variables are skipped: the frontend never has their values, so
 * `{{key}}` stays visible, and the backend fills them in when sending.
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

  /**
   * Returns a copy of a request with its URL, headers and body interpolated.
   * The original (raw) object is left untouched so callers can persist it
   * without writing resolved values back to the database.
   */
  interpolateRequest<T extends { url: string; body: string; request_headers: string }>(
    request: T,
  ): T {
    return {
      ...request,
      url: this.interpolate(request.url),
      body: this.interpolate(request.body),
      request_headers: this.interpolate(request.request_headers),
    };
  }
}
