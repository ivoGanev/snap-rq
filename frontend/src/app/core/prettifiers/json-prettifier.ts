import { Prettifier } from './prettifier';

/**
 * Prettifier for JSON response bodies.
 *
 * Detects JSON either by Content-Type or by inspecting the body structure, then
 * reformats it with 2-space indentation. Invalid JSON is returned unchanged.
 */
export class JsonPrettifier extends Prettifier {
  readonly name = 'json';

  canPrettify(raw: string, contentType?: string): boolean {
    if (contentType?.includes('json')) {
      return true;
    }

    const trimmed = raw.trim();
    if (trimmed.length === 0) {
      return false;
    }

    return (
      (trimmed.startsWith('{') && trimmed.endsWith('}')) ||
      (trimmed.startsWith('[') && trimmed.endsWith(']'))
    );
  }

  prettify(raw: string): string {
    try {
      return JSON.stringify(JSON.parse(raw), null, 2);
    } catch {
      return raw;
    }
  }
}
