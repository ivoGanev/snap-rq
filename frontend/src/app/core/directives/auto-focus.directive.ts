import { AfterViewInit, Directive, ElementRef, inject } from '@angular/core';

/** Focuses the element when it appears; the autofocus attribute only works on page load. */
@Directive({ selector: '[autoFocus]' })
export class AutoFocus implements AfterViewInit {
  private readonly el = inject<ElementRef<HTMLElement>>(ElementRef);

  ngAfterViewInit(): void {
    this.el.nativeElement.focus();
  }
}
