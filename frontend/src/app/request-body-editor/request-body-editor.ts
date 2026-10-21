import { ChangeDetectionStrategy, Component, input, output, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { CodeEditor, type EditorVariable, type EditorLanguage } from '../code-editor/code-editor';

/**
 * Shared body editor UI used by both the full request editor and the bottom
 * drawer body tab.
 */
@Component({
  selector: 'app-request-body-editor',
  imports: [FormsModule, CodeEditor],
  templateUrl: './request-body-editor.html',
  styleUrl: './request-body-editor.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class RequestBodyEditor {
  readonly body = input<string>('');
  readonly type = input<EditorLanguage>('json');
  readonly variables = input<EditorVariable[]>([]);
  readonly showLabel = input<boolean>(true);

  readonly bodyChange = output<string>();
  readonly typeChange = output<EditorLanguage>();

  private readonly codeEditor = viewChild(CodeEditor);

  setType(type: EditorLanguage): void {
    this.typeChange.emit(type);
  }

  formatCurrent(): void {
    this.codeEditor()?.formatCurrent();
  }

  onTextChange(text: string): void {
    this.bodyChange.emit(text);
  }
}
