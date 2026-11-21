import type { HttpResponse, ScriptRun } from './http';
import type { ScriptProblem } from './script-problem';

/** The hook a try runs. */
export type TryPhase = 'beforeRequest' | 'afterResponse';

/** What trying a script's hook did (script.try). Nothing is sent. */
export interface ScriptTryResult {
  /** The code doesn't compile; nothing ran. */
  problems: ScriptProblem[] | null;
  /** Why nothing (more) could run: no such hook, no saved response… */
  note?: string;
  /** beforeRequest: the request as it would be sent (secret values masked). */
  request?: { method: string; url: string; headers: string; body: string };
  /** The script stopped the send: its error, or ctx.skip's reason. */
  stopped?: string;
  /** afterResponse: the saved response it ran against. */
  response?: { id: number; status_code: number; created_at: string };
  log: ScriptRun[] | null;
}

/** What the "Try it" panel shows: a try, or a real send's response. */
export type TryOutcome =
  | { kind: 'try'; phase: TryPhase; result: ScriptTryResult }
  | { kind: 'sent'; response: HttpResponse };
