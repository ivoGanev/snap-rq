/**
 * Base class for all response body prettifiers.
 *
 * A prettifier decides whether it can improve the presentation of a raw text
 * response and, if so, returns a formatted version. Implementations must not
 * throw: when formatting fails they should return the original raw text.
 */
export abstract class Prettifier {
  abstract readonly name: string;

  /**
   * Returns true when this prettifier understands the given raw text.
   * @param raw The raw response body.
   * @param contentType Optional content-type hint parsed from response headers.
   */
  abstract canPrettify(raw: string, contentType?: string): boolean;

  /**
   * Returns a pretty-printed version of the raw text, or the raw text itself
   * when formatting is not possible.
   */
  abstract prettify(raw: string): string;
}
