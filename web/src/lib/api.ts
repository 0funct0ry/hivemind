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
  role: string
  is_bot: boolean
  status: string
}

export interface Channel {
  id: string
  kind: 'public' | 'private' | 'dm'
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
  kind: 'dm'
  peer: User
  last_message_id: string | null
  last_read_message_id: string | null
}

export interface UnreadEntry {
  channel_id: string
  unread_count: number
  mention_count: number
  last_message_id: string | null
  last_read_message_id: string | null
  joined: boolean
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
  listDMs: () => request<{ data: DM[] }>('/dms'),
  unreadSummary: () => request<{ data: UnreadEntry[] }>('/unreads'),
}
