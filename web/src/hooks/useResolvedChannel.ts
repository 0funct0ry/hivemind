import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { api } from '../lib/api'

export interface ResolvedChannel {
  id: string
  name: string
  topic: string
  kind: 'public' | 'private' | 'dm' | null
  lastReadMessageId: string | null
  isLoading: boolean
}

/** Resolves a `#slug` route param to its channel id + read watermark from the cached channel list. */
export function useChannelBySlug(slug: string | undefined): ResolvedChannel {
  const query = useQuery({ queryKey: ['channels'], queryFn: api.listChannels })
  const channel = query.data?.data.find((c) => c.slug === slug)
  return {
    id: channel?.id ?? '',
    name: channel?.name ?? slug ?? '',
    topic: channel?.topic ?? '',
    kind: channel?.kind ?? null,
    lastReadMessageId: channel?.last_read_message_id ?? null,
    isLoading: query.isLoading,
  }
}

/** Resolves a `@username` route param to its DM channel id, creating the DM on first open. */
export function useDmByUsername(username: string | undefined): ResolvedChannel {
  const queryClient = useQueryClient()
  const dmsQuery = useQuery({ queryKey: ['dms'], queryFn: api.listDMs })
  const [creating, setCreating] = useState(false)

  const dm = dmsQuery.data?.data.find((d) => d.peer.username === username)

  useEffect(() => {
    if (dm || creating || !username || dmsQuery.isLoading) return
    setCreating(true)
    void (async () => {
      try {
        const users = await api.listUsers({ q: username, limit: 5 })
        const peer = users.data.find((u) => u.username === username)
        if (peer) {
          await api.createDM(peer.id)
          await queryClient.invalidateQueries({ queryKey: ['dms'] })
        }
      } finally {
        setCreating(false)
      }
    })()
  }, [dm, creating, username, dmsQuery.isLoading, queryClient])

  return {
    id: dm?.id ?? '',
    name: dm?.peer.display_name || dm?.peer.username || username || '',
    topic: '',
    kind: dm ? 'dm' : null,
    lastReadMessageId: dm?.last_read_message_id ?? null,
    isLoading: dmsQuery.isLoading || (!dm && creating),
  }
}
