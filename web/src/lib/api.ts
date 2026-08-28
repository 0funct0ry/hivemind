export class ApiError extends Error {
  code: string
  field: unknown

  constructor(code: string, message: string, field: unknown = null) {
    super(message)
    this.code = code
    this.field = field
  }
}

export interface User {
  id: string
  username: string
  email: string
  display_name: string
  avatar_color: string
  avatar_url?: string
  role: string
  is_bot: boolean
  status: string
  created_at?: number
  online?: boolean
}

export interface Channel {
  id: string
  kind: 'public' | 'private' | 'dm' | 'group_dm'
  slug: string | null
  name: string
  topic: string
  member_count: number
  last_message_id: string | null
  last_read_message_id: string | null
  joined: boolean
}

export interface DM {
  id: string
  kind: 'dm' | 'group_dm'
  name?: string
  peer?: User
  members?: User[]
  last_message_id: string | null
  last_read_message_id: string | null
}

export interface UnreadEntry {
  channel_id: string
  unread_count: number
  has_mention: boolean
}

export interface UnreadsResponse {
  channels: UnreadEntry[]
  total_unread: number
  total_mentions: number
}

export interface MessageUser {
  id: string
  username: string
  display_name: string
  avatar_color: string
  avatar_url?: string
  is_bot: boolean
}

export interface Attachment {
  id: string
  name: string
  mime: string
  size: number
  url: string
  width?: number
  height?: number
}

export interface Message {
  id: string
  channel_id: string
  user_id: string
  user: MessageUser | null
  body: string
  thread_id: string | null
  reply_count: number
  last_reply_id: string | null
  has_attachments: boolean
  broadcast: boolean
  attachments: Attachment[]
  edited_at: number | null
  deleted_at: number | null
  created_at: number
  client_msg_id: string | null
  mentions: unknown[]
  /** Client-only: present while an optimistic send is in flight or has failed. */
  status?: 'sending' | 'failed'
}

export interface ActivityResponse {
  from: number
  to: number
  bucket_ms: number
  counts: number[]
  bucket_message_ids: (string | null)[]
  mentions: { bucket: number; message_id: string }[]
  unread_boundary: { bucket: number; message_id: string } | null
  max: number
}

export interface SearchChannel {
  id: string
  kind: 'public' | 'private' | 'dm' | 'group_dm'
  slug: string | null
  name: string
  topic: string
  last_message_id: string | null
}

export interface SearchHit {
  message: Message
  channel: SearchChannel
  snippet: string
}

export interface UploadedFile {
  id: string
  sha256: string
  name: string
  mime: string
  size: number
  uploaded_by: string
  created_at: number
  width?: number
  height?: number
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`/api/v1${path}`, {
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) },
    ...init,
  })

  if (res.status === 401 && !path.startsWith('/auth/login')) {
    if (typeof window !== 'undefined' && window.location.pathname !== '/login') {
      window.location.href = '/login'
    }
  }

  if (!res.ok) {
    let code = 'unknown_error'
    let message = `Request failed with status ${res.status}`
    let field: unknown = null
    try {
      const body = await res.json()
      code = body?.error?.code ?? code
      message = body?.error?.message ?? message
      field = body?.error?.field ?? null
    } catch {
      // response body wasn't JSON; fall back to the generic message above
    }
    throw new ApiError(code, message, field)
  }

  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

export const api = {
  login: (login: string, password: string) =>
    request<{ user: User }>('/auth/login', { method: 'POST', body: JSON.stringify({ login, password }) }),
  logout: () => request<void>('/auth/logout', { method: 'POST' }),
  me: () => request<{ user: User; workspace: { name: string } }>('/auth/me'),
  setup: (token: string, username: string, email: string, password: string) =>
    request<{ user: User }>('/setup', { method: 'POST', body: JSON.stringify({ token, username, email, password }) }),
  listChannels: () => request<{ data: Channel[] }>('/channels'),
  listJoinableChannels: () => request<{ data: Channel[] }>('/channels?joinable=true'),
  createChannel: (body: { kind: 'public' | 'private'; slug: string; name: string; topic?: string }) => request<{ channel: Channel }>('/channels', { method: 'POST', body: JSON.stringify(body) }),
  joinChannel: (id: string) => request<void>(`/channels/${id}/join`, { method: 'POST' }),
  leaveChannel: (id: string) => request<void>(`/channels/${id}/leave`, { method: 'POST' }),
  updateMe: (patch: { display_name?: string; avatar_file_id?: string | null }) => request<{ user: User }>('/users/me', { method: 'PATCH', body: JSON.stringify(patch) }),
  listDMs: () => request<{ data: DM[] }>('/dms'),
  recentDMs: () => request<{ data: User[] }>('/dms?recent=1'),
  hideDM: (channelId: string) => request<void>(`/dms/${channelId}/hide`, { method: 'POST' }),
  unreadSummary: () => request<UnreadsResponse>('/unreads'),
  getUser: (id: string) => request<{ user: User }>(`/users/${id}`),
  listUsers: (params: { q?: string; channelId?: string; limit?: number } = {}) => {
    const qs = new URLSearchParams()
    if (params.q) qs.set('q', params.q)
    if (params.channelId) qs.set('channel_id', params.channelId)
    if (params.limit) qs.set('limit', String(params.limit))
    return request<{ data: User[] }>(`/users?${qs.toString()}`)
  },
  createDM: (userIds: string[]) =>
    request<{ channel: DM }>('/dms', { method: 'POST', body: JSON.stringify({ user_ids: userIds }) }),
  listChannelMembers: (channelId: string) => request<{ data: User[] }>(`/channels/${channelId}/members`),
  getPresence: () => request<{ online: string[] }>('/presence'),

  listMessages: (
    channelId: string,
    params: { before?: string; after?: string; around?: string; limit?: number } = {},
  ) => {
    const qs = new URLSearchParams()
    if (params.around) qs.set('around', params.around)
    else {
      if (params.before) qs.set('before', params.before)
      if (params.after) qs.set('after', params.after)
    }
    if (params.limit) qs.set('limit', String(params.limit))
    return request<{ data: Message[]; has_more: boolean; next_before: string }>(
      `/channels/${channelId}/messages?${qs.toString()}`,
    )
  },
  getActivity: (channelId: string, params: { buckets?: number; from?: number; to?: number } = {}) => {
    const qs = new URLSearchParams()
    if (params.buckets) qs.set('buckets', String(params.buckets))
    if (params.from) qs.set('from', String(params.from))
    if (params.to) qs.set('to', String(params.to))
    return request<ActivityResponse>(`/channels/${channelId}/activity?${qs.toString()}`)
  },
  search: (params: { q: string; in?: string; from?: string; has?: string; before?: string; limit?: number }) => {
    const qs = new URLSearchParams()
    qs.set('q', params.q)
    if (params.in) qs.set('channel', params.in)
    if (params.from) qs.set('from', params.from)
    if (params.has) qs.set('has', params.has)
    if (params.before) qs.set('before', params.before)
    if (params.limit) qs.set('limit', String(params.limit))
    return request<{ data: SearchHit[]; has_more: boolean; next_before: string | null }>(
      `/search?${qs.toString()}`,
    )
  },
  createMessage: (
    channelId: string,
    body: { body: string; thread_id?: string; client_msg_id?: string; file_ids?: string[]; also_send_to_channel?: boolean },
  ) => request<{ message: Message }>(`/channels/${channelId}/messages`, { method: 'POST', body: JSON.stringify(body) }),
  getMessage: (id: string) => request<{ message: Message }>(`/messages/${id}`),
  listReplies: (rootId: string, params: { after?: string; limit?: number } = {}) => {
    const qs = new URLSearchParams()
    if (params.after) qs.set('after', params.after)
    if (params.limit) qs.set('limit', String(params.limit))
    return request<{ root: Message; data: Message[]; has_more: boolean }>(`/messages/${rootId}/replies?${qs.toString()}`)
  },
  markRead: (channelId: string, messageId: string) =>
    request<void>(`/channels/${channelId}/read`, { method: 'POST', body: JSON.stringify({ message_id: messageId }) }),

  uploadFile: (file: File, onProgress?: (pct: number) => void): Promise<UploadedFile> =>
    new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest()
      xhr.open('POST', '/api/v1/uploads')
      xhr.withCredentials = true
      xhr.upload.onprogress = (evt) => {
        if (evt.lengthComputable && onProgress) onProgress((evt.loaded / evt.total) * 100)
      }
      xhr.onload = () => {
        let body: unknown = null
        try {
          body = JSON.parse(xhr.responseText)
        } catch {
          // ignore
        }
        if (xhr.status >= 200 && xhr.status < 300) {
          resolve(body as UploadedFile)
        } else {
          const err = body as { error?: { code?: string; message?: string; field?: unknown } } | null
          reject(new ApiError(err?.error?.code ?? 'unknown_error', err?.error?.message ?? 'Upload failed.', err?.error?.field ?? null))
        }
      }
      xhr.onerror = () => reject(new ApiError('network_error', 'Upload failed.'))
      const form = new FormData()
      form.append('file', file)
      xhr.send(form)
    }),
  uploadAvatar: (file: File) => api.uploadFile(file),
}
