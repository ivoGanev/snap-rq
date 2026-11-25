import { describe, expect, it } from 'vitest';
import { diffRange } from './diff-range';

// Revealing a secret edits the body in place with this one span; a wrong
// span would corrupt the text shown, or replace more than needed and drop
// the folds and scroll position the in-place edit exists to keep.

function apply(before: string, after: string): string {
  const change = diffRange(before, after);
  if (!change) return before;
  return before.slice(0, change.from) + change.insert + before.slice(change.to);
}

const masked = '{\n  "user": "demo-user",\n  "password": "•••••",\n  "id": 42\n}';
const revealed = '{\n  "user": "demo-user",\n  "password": "hunter2-long-value",\n  "id": 42\n}';

describe('diffRange', () => {
  it('is null when nothing changed', () => {
    expect(diffRange(masked, masked)).toBeNull();
  });

  it('replaces only the secret when revealing and hiding', () => {
    expect(diffRange(masked, revealed)).toEqual({
      from: masked.indexOf('•'),
      to: masked.indexOf('•') + 5,
      insert: 'hunter2-long-value',
    });
    expect(apply(masked, revealed)).toBe(revealed);
    expect(apply(revealed, masked)).toBe(masked);
  });

  it('spans from the first to the last of several secrets', () => {
    const before = 'a=•••••\nb=plain\nc=•••••';
    const after = 'a=one\nb=plain\nc=three';
    const change = diffRange(before, after)!;
    expect(change.from).toBe(2);
    expect(change.to).toBe(before.length);
    expect(apply(before, after)).toBe(after);
  });

  it('handles changes at the very start or end', () => {
    for (const [before, after] of [
      ['secret rest', 'XYZ rest'],
      ['rest secret', 'rest XYZ'],
      ['', 'all new'],
      ['all gone', ''],
    ]) {
      expect(apply(before, after)).toBe(after);
    }
  });

  it('never counts a character as both shared start and shared end', () => {
    for (const [before, after] of [
      ['aa', 'aaa'],
      ['aaa', 'aa'],
      ['abab', 'ab'],
      ['x', 'xx'],
    ]) {
      const change = diffRange(before, after)!;
      expect(change.from).toBeLessThanOrEqual(change.to);
      expect(apply(before, after)).toBe(after);
    }
  });
});
