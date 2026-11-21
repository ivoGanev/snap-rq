import type { Completion, CompletionContext, CompletionResult, CompletionSource } from '@codemirror/autocomplete';
import { SCRIPT_API, SCRIPT_GLOBALS, type ScriptApiMember } from './script-api';

const toCompletion = (member: ScriptApiMember): Completion => ({
  label: member.label,
  type: member.type,
  detail: member.detail,
  info: member.info,
});

/**
 * Completions for the script editor: the script API after "req.",
 * "ctx.env." and so on, the API's names at the start of a word, and inside
 * ctx.env.get('…') the selected environment's variable names.
 */
export function scriptCompletions(envKeys: () => string[]): CompletionSource {
  return (context: CompletionContext): CompletionResult | null => {
    // Inside ctx.env.get('…') or "…": the environment's variable names.
    const envArg = context.matchBefore(/ctx\.env\.get\(\s*['"][\w-]*/);
    if (envArg) {
      const from = envArg.from + envArg.text.search(/['"]/) + 1;
      return {
        from,
        options: envKeys().map((key) => ({ label: key, type: 'variable' })),
        validFor: /^[\w-]*$/,
      };
    }

    // "<path>.<partial>": that path's members. ctx.expect(…) counts as one path.
    const member = context.matchBefore(/[\w$]+(?:\.[\w$]+)*(?:\([^()]*\))?\.[\w$]*/);
    if (member) {
      const dot = member.text.lastIndexOf('.');
      const path = member.text.slice(0, dot).replace(/^ctx\.expect\(.*\)$/, 'ctx.expect()');
      const members = SCRIPT_API[path];
      if (!members) return null;
      return { from: member.from + dot + 1, options: members.map(toCompletion), validFor: /^[\w$]*$/ };
    }

    // A word on its own: the API's names (JavaScript's own and the script's
    // variables come from the JavaScript language's completions).
    const word = context.matchBefore(/[\w$]+/);
    if (!word && !context.explicit) return null;
    return {
      from: word?.from ?? context.pos,
      options: SCRIPT_GLOBALS.map(toCompletion),
      validFor: /^[\w$]*$/,
    };
  };
}
