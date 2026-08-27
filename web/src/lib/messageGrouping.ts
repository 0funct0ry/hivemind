export const GROUP_WINDOW_MS = 5 * 60 * 1000

/** Whether `curr` should be rendered as a continuation of `prev` — same author, within the
 * grouping window, and not interrupted by a divider. */
export function shouldGroup(
  prevAuthorId: string | null,
  prevTs: number,
  currAuthorId: string,
  currTs: number,
  dividerBetween: boolean,
): boolean {
  return prevAuthorId === currAuthorId && currTs - prevTs < GROUP_WINDOW_MS && !dividerBetween
}
