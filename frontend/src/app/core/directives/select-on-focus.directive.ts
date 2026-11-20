import { AfterViewInit, Directive, ElementRef, inject } from '@angular/core';

/**
 * Focuses and selects the contents of an input the moment it is inserted into
 * the DOM. Used for inline rename inputs so the current name is pre-selected
 * and typing replaces it.
 */
@Directive({
  selector: '[selectOnFocus]',
})
export class SelectOnFocusDirective implements AfterViewInit {
  private readonly el = inject(ElementRef<HTMLInputElement | HTMLTextAreaElement>);

  ngAfterViewInit(): void {
    const input = this.el.nativeElement;
    input.focus();
    // `[ngModel]` writes the value in a microtask after the first render, so
    // selecting now would select an empty field. Select once that write is done.
    void Promise.resolve().then(() => {
      if (document.activeElement === input) input.select();
    });
  }
}
