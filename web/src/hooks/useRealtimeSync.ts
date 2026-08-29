import { useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { wsClient } from '../lib/ws'
import { useUiStore } from '../store/ui'
import { bumpActivityBucket } from '../components/PulseRuler'

interface MessageCreatedPayload {
  channel_id: string
}

interface ThreadReplyPayload {
  channel_id: string
  root_id: string
}

interface MessageUpdatedPayload {
  id: string
  channel_id: string
  thread_id: string | null
}

interface MessageDeletedPayload {
  id: string
  channel_id: string
  thread_id: string | null
}

interface TypingPayload {
  channel_id: string
  user_id: string
  expires_at: number
}

interface PresenceChangedPayload {
  user_id: string
  online: boolean
}

/**
 * Connects the WebSocket client for the lifetime of the app shell and wires
 * server events onto TanStack Query cache invalidation. Messages themselves
 * are never delivered over the socket (SPEC.md §5) — events here only ever
 * tell the client "something changed, go refetch".
 */
export function useRealtimeSync() {
  const queryClient = useQueryClient()
  const setConnectionState = useUiStore((s) => s.setConnectionState)
  const setTyping = useUiStore((s) => s.setTyping)

  useEffect(() => {
    const offState = wsClient.onStateChange(setConnectionState)
    const invalidate = (keys: string[]) => () => {
      for (const key of keys) queryClient.invalidateQueries({ queryKey: [key] })
    }
    const offMessage = wsClient.on('message.created', (payload) => {
      const p = payload as MessageCreatedPayload
      queryClient.invalidateQueries({ queryKey: ['messages', p.channel_id] })
      queryClient.invalidateQueries({ queryKey: ['channels'] })
      queryClient.invalidateQueries({ queryKey: ['unreads'] })
      bumpActivityBucket(queryClient, p.channel_id)
    })
    const offThread = wsClient.on('thread.reply', (payload) => {
      const p = payload as ThreadReplyPayload
      queryClient.invalidateQueries({ queryKey: ['thread', p.root_id] })
      queryClient.invalidateQueries({ queryKey: ['messages', p.channel_id] })
      queryClient.invalidateQueries({ queryKey: ['channels'] })
      queryClient.invalidateQueries({ queryKey: ['unreads'] })
    })
    const offMessageUpdated = wsClient.on('message.updated', (payload) => {
      const p = payload as MessageUpdatedPayload
      queryClient.invalidateQueries({ queryKey: ['messages', p.channel_id] })
      if (p.thread_id) queryClient.invalidateQueries({ queryKey: ['thread', p.thread_id] })
    })
    const offMessageDeleted = wsClient.on('message.deleted', (payload) => {
      const p = payload as MessageDeletedPayload
      queryClient.invalidateQueries({ queryKey: ['messages', p.channel_id] })
      // A deleted message may itself be an open thread's root — invalidate that key too so
      // the thread panel derives its locked state from the refetched root's deleted_at.
      queryClient.invalidateQueries({ queryKey: ['thread', p.thread_id ?? p.id] })
    })
    const offRead = wsClient.on('read.updated', invalidate(['unreads']))
    const offChannel = wsClient.on('channel.created', invalidate(['channels']))
    const offChannelUpdated = wsClient.on('channel.updated', invalidate(['channels']))
    const offMember = wsClient.on('member.joined', invalidate(['channels']))
    const offMemberLeft = wsClient.on('member.left', invalidate(['channels']))
    const offMention = wsClient.on('mention.created', invalidate(['unreads']))
    const offTyping = wsClient.on('typing', (payload) => {
      const p = payload as TypingPayload
      setTyping(p.channel_id, { userId: p.user_id, name: p.user_id, expiresAt: p.expires_at })
    })
    const offPresence = wsClient.on('presence.changed', (payload) => {
      const p = payload as PresenceChangedPayload
      queryClient.setQueryData<{ online: string[] }>(['presence'], (old) => {
        const online = new Set(old?.online ?? [])
        if (p.online) online.add(p.user_id)
        else online.delete(p.user_id)
        return { online: Array.from(online) }
      })
    })

    wsClient.connect()

    return () => {
      offState()
      offMessage()
      offThread()
      offMessageUpdated()
      offMessageDeleted()
      offRead()
      offChannel()
      offChannelUpdated()
      offMember()
      offMemberLeft()
      offMention()
      offTyping()
      offPresence()
      wsClient.close()
    }
  }, [queryClient, setConnectionState, setTyping])
}
