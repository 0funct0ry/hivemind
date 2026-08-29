import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type Message } from '../lib/api'

function newClientMsgId(): string {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) return crypto.randomUUID()
  return `${Date.now()}-${Math.random().toString(36).slice(2)}`
}

export function useMessages(channelId: string | undefined) {
  const query = useInfiniteQuery({
    queryKey: ['messages', channelId],
    queryFn: ({ pageParam }) => api.listMessages(channelId!, { before: pageParam, limit: 50 }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => (last.has_more ? last.next_before : undefined),
    enabled: !!channelId,
  })

  const messages = (query.data?.pages ?? []).flatMap((p) => p.data)
  return { ...query, messages }
}

export function useThread(rootId: string | null) {
  return useQuery({
    queryKey: ['thread', rootId],
    queryFn: () => api.listReplies(rootId!, { limit: 200 }),
    enabled: !!rootId,
  })
}

interface SendInput {
  body: string
  threadId?: string
  fileIds?: string[]
  alsoSendToChannel?: boolean
}

export function useSendMessage(channelId: string | undefined) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (input: SendInput & { clientMsgId: string }) => {
      const res = await api.createMessage(channelId!, {
        body: input.body,
        thread_id: input.threadId,
        client_msg_id: input.clientMsgId,
        file_ids: input.fileIds,
        also_send_to_channel: input.alsoSendToChannel,
      })
      return res.message
    },
    onMutate: async (input) => {
      if (!channelId) return
      const optimistic: Message = {
        id: `optimistic-${input.clientMsgId}`,
        channel_id: channelId,
        user_id: '',
        user: null,
        body: input.body,
        thread_id: input.threadId ?? null,
        reply_count: 0,
        last_reply_id: null,
        has_attachments: (input.fileIds?.length ?? 0) > 0,
        broadcast: !!input.alsoSendToChannel,
        attachments: [],
        edited_at: null,
        deleted_at: null,
        deleted_by: null,
        created_at: Date.now(),
        client_msg_id: input.clientMsgId,
        mentions: [],
        reactions: [],
        status: 'sending',
      }
      const key = input.threadId ? ['thread', input.threadId] : ['messages', channelId]
      queryClient.setQueryData(key, (old: unknown) => appendOptimistic(old, input.threadId, optimistic))
    },
    onSuccess: (message, input) => {
      const key = input.threadId ? ['thread', input.threadId] : ['messages', channelId]
      queryClient.setQueryData(key, (old: unknown) => reconcile(old, input.threadId, input.clientMsgId, message))
      queryClient.invalidateQueries({ queryKey: ['channels'] })
      queryClient.invalidateQueries({ queryKey: ['unreads'] })
    },
    onError: (_err, input) => {
      const key = input.threadId ? ['thread', input.threadId] : ['messages', channelId]
      queryClient.setQueryData(key, (old: unknown) => markFailed(old, input.threadId, input.clientMsgId))
    },
  })
}

export function useEditMessage(channelId: string | undefined) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (input: { id: string; body: string }) => api.updateMessage(input.id, input.body).then((r) => r.message),
    onSuccess: () => {
      // The message itself updates via the message.updated WS event, like any other viewer —
      // this keeps a single source of truth for the rendered message.
      if (channelId) queryClient.invalidateQueries({ queryKey: ['messages', channelId] })
    },
  })
}

export function useDeleteMessage(channelId: string | undefined) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (id: string) => api.deleteMessage(id).then((r) => r.message),
    onSuccess: () => {
      if (channelId) queryClient.invalidateQueries({ queryKey: ['messages', channelId] })
      queryClient.invalidateQueries({ queryKey: ['thread'] })
    },
  })
}

interface ToggleReactionInput {
  messageId: string
  emoji: string
  action: 'add' | 'remove'
  threadId?: string
}

/** Applies `updater` to a message's `reactions` array wherever it appears — the channel's
 * infinite message pages, and the open thread's data, if any — since a message shown in both
 * places is patched by the same event/mutation with no shared object identity to key on. */
function patchMessageReactions(
  queryClient: ReturnType<typeof useQueryClient>,
  channelId: string | undefined,
  threadId: string | undefined,
  messageId: string,
  updater: (m: Message) => Message,
) {
  if (channelId) {
    queryClient.setQueryData(['messages', channelId], (old: unknown) => {
      const infinite = old as { pages: Array<{ data: Message[] }> } | undefined
      if (!infinite) return infinite
      return {
        ...infinite,
        pages: infinite.pages.map((p) => ({
          ...p,
          data: p.data.map((m) => (m.id === messageId ? updater(m) : m)),
        })),
      }
    })
  }
  if (threadId) {
    queryClient.setQueryData(['thread', threadId], (old: unknown) => {
      const t = old as { root: Message; data: Message[] } | undefined
      if (!t) return t
      return {
        ...t,
        root: t.root.id === messageId ? updater(t.root) : t.root,
        data: t.data.map((m) => (m.id === messageId ? updater(m) : m)),
      }
    })
  }
}

function applyReactionToggle(m: Message, emoji: string, userId: string, action: 'add' | 'remove'): Message {
  const reactions = m.reactions.map((r) => ({ ...r, user_ids: [...r.user_ids] }))
  const existing = reactions.find((r) => r.emoji === emoji)
  if (action === 'add') {
    if (existing) {
      if (!existing.user_ids.includes(userId)) existing.user_ids.push(userId)
    } else {
      reactions.push({ emoji, user_ids: [userId] })
    }
  } else if (existing) {
    existing.user_ids = existing.user_ids.filter((id) => id !== userId)
  }
  return { ...m, reactions: reactions.filter((r) => r.user_ids.length > 0) }
}

export function useToggleReaction(channelId: string | undefined, currentUserId: string | undefined) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (input: ToggleReactionInput) =>
      input.action === 'add' ? api.addReaction(input.messageId, input.emoji) : api.removeReaction(input.messageId, input.emoji),
    onMutate: (input) => {
      if (!currentUserId) return
      patchMessageReactions(queryClient, channelId, input.threadId, input.messageId, (m) =>
        applyReactionToggle(m, input.emoji, currentUserId, input.action),
      )
    },
    onError: (_err, input) => {
      if (!currentUserId) return
      // Revert by applying the inverse action — good enough for a single-user toggle undo,
      // and avoids snapshotting the entire cache shape just for this one mutation.
      const inverse = input.action === 'add' ? 'remove' : 'add'
      patchMessageReactions(queryClient, channelId, input.threadId, input.messageId, (m) =>
        applyReactionToggle(m, input.emoji, currentUserId, inverse),
      )
    },
    onSuccess: (_data, input) => {
      if (channelId) queryClient.invalidateQueries({ queryKey: ['messages', channelId] })
      if (input.threadId) queryClient.invalidateQueries({ queryKey: ['thread', input.threadId] })
    },
  })
}

function appendOptimistic(old: unknown, threadId: string | undefined, msg: Message) {
  if (threadId) {
    const t = old as { root: Message; data: Message[]; has_more: boolean } | undefined
    if (!t) return t
    return { ...t, data: [...t.data, msg] }
  }
  const infinite = old as { pages: Array<{ data: Message[]; has_more: boolean; next_before: string }>; pageParams: unknown[] } | undefined
  if (!infinite || infinite.pages.length === 0) {
    return { pages: [{ data: [msg], has_more: false, next_before: '' }], pageParams: [undefined] }
  }
  const pages = [...infinite.pages]
  const last = pages.length - 1
  pages[last] = { ...pages[last], data: [...pages[last].data, msg] }
  return { ...infinite, pages }
}

function replaceMessage(list: Message[], clientMsgId: string, replacement: Message | null, status?: Message['status']) {
  return list.map((m) => {
    if (m.client_msg_id !== clientMsgId || !m.id.startsWith('optimistic-')) return m
    if (replacement) return replacement
    return { ...m, status }
  })
}

function reconcile(old: unknown, threadId: string | undefined, clientMsgId: string, message: Message) {
  if (threadId) {
    const t = old as { root: Message; data: Message[]; has_more: boolean } | undefined
    if (!t) return t
    return { ...t, data: replaceMessage(t.data, clientMsgId, message) }
  }
  const infinite = old as { pages: Array<{ data: Message[]; has_more: boolean; next_before: string }>; pageParams: unknown[] } | undefined
  if (!infinite) return infinite
  return { ...infinite, pages: infinite.pages.map((p) => ({ ...p, data: replaceMessage(p.data, clientMsgId, message) })) }
}

function markFailed(old: unknown, threadId: string | undefined, clientMsgId: string) {
  if (threadId) {
    const t = old as { root: Message; data: Message[]; has_more: boolean } | undefined
    if (!t) return t
    return { ...t, data: replaceMessage(t.data, clientMsgId, null, 'failed') }
  }
  const infinite = old as { pages: Array<{ data: Message[]; has_more: boolean; next_before: string }>; pageParams: unknown[] } | undefined
  if (!infinite) return infinite
  return { ...infinite, pages: infinite.pages.map((p) => ({ ...p, data: replaceMessage(p.data, clientMsgId, null, 'failed') })) }
}

export { newClientMsgId }
