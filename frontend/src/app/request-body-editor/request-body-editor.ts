import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { BodyEditor, type BodyEditorVariable, type BodyType } from '../body-editor/body-editor';

/**
 * Shared body editor UI used by both the full request editor and the bottom
 * drawer body tab.
 */
@Component({
  selector: 'app-request-body-editor',
  imports: [FormsModule, BodyEditor],
  templateUrl: './request-body-editor.html',
  styleUrl: './request-body-editor.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class RequestBodyEditor {
  readonly body = input<string>('');
  readonly type = input<BodyType>('json');
  readonly variables = input<BodyEditorVariable[]>([]);
  readonly showLabel = input<boolean>(true);

  readonly bodyChange = output<string>();
  readonly typeChange = output<BodyType>();

  setType(type: BodyType): void {
    this.typeChange.emit(type);
  }

  onTextChange(text: string): void {
    this.bodyChange.emit(text);
  }
}
