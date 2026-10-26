export interface EnvironmentVariableValue {
  key: string;
  value: string;
}

const VARIABLE_REGEX = /\{\{\s*([a-zA-Z0-9_-]+)\s*\}\}/g;

/**
 * An environment resolved into an interpolation context.
 *
 * This is the single implementation of `{{variable}}` interpolation in the
 * app: the frontend owns the environment's variables in memory (loaded on
 * environment selection and kept in sync by the variables editor), so display
 * and request execution both resolve through this entity. The backend executes
 * whatever content it is given and never interpolates itself.
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
    return text.replace(VARIABLE_REGEX, (match, key: string) => {
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
