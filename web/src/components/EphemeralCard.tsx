import { type CommandExecResult } from '../lib/api'

/** Renders a slash command's ephemeral response — client-only view state that is never
 * persisted, never broadcast, and gone on dismiss or reload (SPEC.md §3.3). `failed` switches to
 * the red-accented warning treatment for a timeout/error result (UJM edge case 1). */
export function EphemeralCard({
  result,
  failed,
  onDismiss,
}: {
  result: CommandExecResult
  failed?: boolean
  onDismiss: () => void
}) {
  return (
    <div
      className={
        'mb-1.5 flex items-start gap-2 rounded-md border px-2.5 py-2 text-[13px] ' +
        (failed ? 'border-red-300 bg-red-50 text-red-700' : 'border-rule bg-paper-2 text-ink')
      }
    >
      <div className="flex-1">
        <div className="mb-0.5 font-mono text-[9.5px] uppercase tracking-wide text-ink-3">Only visible to you</div>
        <div className="whitespace-pre-wrap">{result.text}</div>
      </div>
      <button
        type="button"
        onClick={onDismiss}
        className="shrink-0 rounded px-1.5 py-0.5 text-xs text-ink-3 hover:bg-paper-3"
      >
        Dismiss
      </button>
    </div>
  )
}
