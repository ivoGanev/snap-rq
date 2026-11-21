import { Component, computed, input, signal } from '@angular/core';
import type { ScriptRun } from '../core/models/http';

/**
 * What a send's scripts did, as saved with the response: per script and
 * phase, its tests (pass/fail), console output and error.
 */
@Component({
  selector: 'app-script-log',
  templateUrl: './script-log.html',
  styleUrl: './script-log.scss',
})
export class ScriptLog {
  readonly runs = input.required<ScriptRun[]>();

  /** The runs can be folded away to the one-line summary. */
  protected readonly expanded = signal(true);

  protected readonly summary = computed(() => {
    let passed = 0;
    let failed = 0;
    let errors = 0;
    for (const run of this.runs()) {
      for (const test of run.tests ?? []) {
        if (test.passed) passed++;
        else failed++;
      }
      if (run.error) errors++;
    }
    return { passed, failed, errors };
  });
}
