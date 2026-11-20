import { AfterViewInit, Directive, ElementRef, inject } from '@angular/core';

/**
 * Focuses and selects the contents of an input the moment it is inserted into
 * the DOM. Used for inline rename inputs so the current name is pre-selected
 * and typing replaces it, without a setTimeout/effect.
 */
@Directive({
  selector: '[selectOnFocus]',
})
export class SelectOnFocusDirective implements AfterViewInit {
  private readonly el = inject(ElementRef<HTMLInputElement>);

  ngAfterViewInit(): void {
    this.el.nativeElement.focus();
    this.el.nativeElement.select();
  }
}
