import { useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { wsClient } from '../lib/ws'
import { useUiStore } from '../store/ui'

/**
 * Connects the WebSocket client for the lifetime of the app shell and wires
 * server events onto TanStack Query cache invalidation. Messages themselves
 * are never delivered over the socket (SPEC.md §5) — events here only ever
 * tell the client "something changed, go refetch".
 */
export function useRealtimeSync() {
  const queryClient = useQueryClient()
  const setConnectionState = useUiStore((s) => s.setConnectionState)

  useEffect(() => {
    const offState = wsClient.onStateChange(setConnectionState)
    const invalidate = (keys: string[]) => () => {
      for (const key of keys) queryClient.invalidateQueries({ queryKey: [key] })
    }
    const offMessage = wsClient.on('message.created', invalidate(['channels', 'unreads']))
    const offThread = wsClient.on('thread.reply', invalidate(['channels', 'unreads']))
    const offRead = wsClient.on('read.updated', invalidate(['unreads']))
    const offChannel = wsClient.on('channel.created', invalidate(['channels']))
    const offChannelUpdated = wsClient.on('channel.updated', invalidate(['channels']))
    const offMember = wsClient.on('member.joined', invalidate(['channels']))
    const offMemberLeft = wsClient.on('member.left', invalidate(['channels']))
    const offMention = wsClient.on('mention.created', invalidate(['unreads']))

    wsClient.connect()

    return () => {
      offState()
      offMessage()
      offThread()
      offRead()
      offChannel()
      offChannelUpdated()
      offMember()
      offMemberLeft()
      offMention()
      wsClient.close()
    }
  }, [queryClient, setConnectionState])
}
