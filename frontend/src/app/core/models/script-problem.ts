/** A mistake in a script's code, at a 1-based line and column (see script.check). */
export interface ScriptProblem {
  line: number;
  column: number;
  message: string;
}
