import { useQuery } from '@tanstack/react-query'
import { api, type Bot } from '../lib/api'
import { formatTimestamp } from './TokenWidgets'
import { Avatar } from './Avatar'
import { Modal } from './Modal'

function InfoRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-4 border-t border-rule py-2 first:border-t-0">
      <span className="shrink-0 text-xs font-medium uppercase tracking-wide text-ink-3">{label}</span>
      <span className="text-right text-sm text-ink">{value}</span>
    </div>
  )
}

/** Read-only detail panel for a bot — every field except its bearer token (shown once at
 * create/regenerate time and never retrievable again, so it deliberately has no place here). */
export function BotInfoModal({ bot, onClose }: { bot: Bot | null; onClose: () => void }) {
  const { data: creatorData } = useQuery({
    queryKey: ['user', bot?.created_by],
    queryFn: () => api.getUser(bot!.created_by),
    enabled: bot !== null,
  })
  const creator = creatorData?.user
  const creatorName = creator ? creator.display_name || creator.username : bot?.created_by

  return (
    <Modal open={bot !== null} onClose={onClose} labelledBy="bot-info-title">
      {bot && (
        <div className="max-h-[80vh] overflow-y-auto p-5">
          <div className="flex items-center gap-3">
            <Avatar name={bot.display_name} color={bot.avatar_color} size={36} />
            <div>
              <h2 id="bot-info-title" className="flex items-center gap-1.5 font-display text-lg font-semibold text-ink">
                {bot.display_name}
                <span className="rounded bg-paper-3 px-1 font-mono text-[8px] text-ink-2">BOT</span>
              </h2>
              <p className="text-xs text-ink-3">{bot.description || 'No description'}</p>
            </div>
          </div>

          <div className="mt-4">
            <InfoRow label="Username" value={<code className="font-mono text-xs">@{bot.username}</code>} />
            <InfoRow label="Status" value={<BotStatusTagInline status={bot.status} />} />
            <InfoRow label="Description" value={bot.description || '—'} />
            <InfoRow label="Created by" value={creatorName} />
            <InfoRow label="Created" value={formatTimestamp(bot.created_at)} />
            <InfoRow label="Last updated" value={formatTimestamp(bot.updated_at)} />
          </div>

          <p className="mt-4 text-xs text-ink-3">
            The bearer token isn't shown here — it's only ever visible once, right after creation
            or regeneration. Use <span className="text-ink-2">Regenerate token</span> if it's been
            lost.
          </p>

          <div className="mt-5 flex justify-end">
            <button
              type="button"
              className="rounded border border-rule px-3 py-1.5 text-sm text-ink hover:bg-paper-3"
              onClick={onClose}
            >
              Close
            </button>
          </div>
        </div>
      )}
    </Modal>
  )
}

function BotStatusTagInline({ status }: { status: Bot['status'] }) {
  if (status === 'active') {
    return <span className="rounded-full bg-teal/10 px-2 py-0.5 text-xs font-medium text-teal">Active</span>
  }
  return <span className="rounded-full bg-paper-3 px-2 py-0.5 text-xs font-medium text-ink-2">Revoked</span>
}
