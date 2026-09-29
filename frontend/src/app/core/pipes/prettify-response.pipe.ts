import { Pipe, type PipeTransform } from '@angular/core';
import { prettifyResponseBody } from '../prettifiers/response-prettifier';

/**
 * Pretty-prints a response body when its content looks like JSON or CSV.
 *
 * Usage:
 *   {{ response.body | prettifyResponse : response.headers : true }}
 */
@Pipe({
  name: 'prettifyResponse',
  standalone: true,
})
export class PrettifyResponsePipe implements PipeTransform {
  transform(body: string, headers: string, enabled = true): string {
    return prettifyResponseBody(body, headers, enabled);
  }
}
