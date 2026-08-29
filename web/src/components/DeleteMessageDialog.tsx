import { useRef } from 'react'
import { Modal } from './Modal'

export function DeleteMessageDialog({
  open,
  onClose,
  onConfirm,
  busy,
}: {
  open: boolean
  onClose: () => void
  onConfirm: () => void
  busy?: boolean
}) {
  const confirmRef = useRef<HTMLButtonElement>(null)

  return (
    <Modal open={open} onClose={onClose} labelledBy="delete-message-title" initialFocusRef={confirmRef}>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          if (!busy) onConfirm()
        }}
        className="p-5"
      >
        <h2 id="delete-message-title" className="font-display text-lg font-semibold text-ink">
          Delete message
        </h2>
        <p className="mt-2 text-sm text-ink-2">
          Are you sure you want to delete this message? This action cannot be undone.
        </p>
        <div className="mt-5 flex gap-2">
          <button
            ref={confirmRef}
            type="submit"
            disabled={busy}
            className="rounded bg-red-600 px-3 py-2 text-sm font-medium text-white hover:bg-red-700 disabled:opacity-50"
          >
            {busy ? 'Deleting…' : 'Confirm'}
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
