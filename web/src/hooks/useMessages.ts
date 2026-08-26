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
        created_at: Date.now(),
        client_msg_id: input.clientMsgId,
        mentions: [],
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
