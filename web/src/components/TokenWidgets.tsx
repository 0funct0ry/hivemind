import { useState } from 'react'

export function formatTimestamp(ms?: number): string {
  if (!ms) return '—'
  return new Date(ms).toLocaleString()
}

export function StatusPill({ disabled }: { disabled: boolean }) {
  return (
    <span
      className={
        'rounded-full px-2 py-0.5 text-xs font-medium ' +
        (disabled ? 'bg-red-100 text-red-700' : 'bg-teal/10 text-teal')
      }
    >
      {disabled ? 'Disabled' : 'Active'}
    </span>
  )
}

/** Shows a freshly (re)issued plaintext secret exactly once — matches the server's
 * shown-once convention for both token creation and rotation. */
export function NewTokenModal({ token, onClose }: { token: string; onClose: () => void }) {
  const [copied, setCopied] = useState(false)
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
      <div className="w-full max-w-md rounded-lg border border-rule bg-white p-6 shadow-lg">
        <h2 className="mb-1 font-display text-lg font-semibold text-ink">New token secret</h2>
        <p className="mb-4 text-sm text-ink-2">
          This is shown once. Copy it now — it cannot be retrieved again.
        </p>
        <code className="mb-4 block break-all rounded border border-rule bg-paper px-3 py-2 text-sm text-ink">
          {token}
        </code>
        <div className="flex justify-end gap-2">
          <button
            className="rounded border border-rule px-3 py-1.5 text-sm text-ink hover:bg-paper"
            onClick={async () => {
              await navigator.clipboard.writeText(token)
              setCopied(true)
            }}
          >
            {copied ? 'Copied' : 'Copy'}
          </button>
          <button className="rounded bg-teal px-3 py-1.5 text-sm text-white hover:opacity-90" onClick={onClose}>
            Done
          </button>
        </div>
      </div>
    </div>
  )
}
