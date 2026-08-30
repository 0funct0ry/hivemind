import { useRef } from 'react'
import { Modal } from './Modal'

/** Generic danger-confirm, same Modal-based pattern as DeleteMessageDialog, generalized with a
 * title/body/confirmLabel so webhook regenerate/delete can reuse it instead of forking. */
export function ConfirmDialog({
  open,
  title,
  body,
  confirmLabel,
  busyLabel,
  onClose,
  onConfirm,
  busy,
}: {
  open: boolean
  title: string
  body: React.ReactNode
  confirmLabel: string
  busyLabel?: string
  onClose: () => void
  onConfirm: () => void
  busy?: boolean
}) {
  const confirmRef = useRef<HTMLButtonElement>(null)

  return (
    <Modal open={open} onClose={onClose} labelledBy="confirm-dialog-title" initialFocusRef={confirmRef}>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          if (!busy) onConfirm()
        }}
        className="p-5"
      >
        <h2 id="confirm-dialog-title" className="font-display text-lg font-semibold text-ink">
          {title}
        </h2>
        <div className="mt-2 text-sm text-ink-2">{body}</div>
        <div className="mt-5 flex gap-2">
          <button
            ref={confirmRef}
            type="submit"
            disabled={busy}
            className="rounded bg-red-600 px-3 py-2 text-sm font-medium text-white hover:bg-red-700 disabled:opacity-50"
          >
            {busy ? busyLabel ?? 'Working…' : confirmLabel}
          </button>
          <button
            type="button"
            onClick={onClose}
            className="rounded border border-rule px-3 py-2 text-sm text-ink-2 hover:bg-paper-3"
          >
            Cancel
          </button>
        </div>
      </form>
    </Modal>
  )
}
