import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';
import { CodeEditor, type EditorVariable } from '../code-editor/code-editor';

/**
 * Shared headers editor UI that reuses the same CodeMirror implementation as
 * the body editor, but without a language selector.
 */
@Component({
  selector: 'app-request-headers-editor',
  imports: [CodeEditor],
  templateUrl: './request-headers-editor.html',
  styleUrl: './request-headers-editor.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class RequestHeadersEditor {
  readonly headers = input<string>('');
  readonly variables = input<EditorVariable[]>([]);
  readonly showLabel = input<boolean>(true);

  readonly headersChange = output<string>();
}
