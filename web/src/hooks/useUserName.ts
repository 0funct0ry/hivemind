import { useQuery } from '@tanstack/react-query'
import { api, type MessageUser } from '../lib/api'

/** Looks up a user's display name by id, cached indefinitely (identities don't change often). */
export function useUserName(userId: string | undefined): string {
  const query = useQuery({
    queryKey: ['user', userId],
    queryFn: () => api.getUser(userId!),
    enabled: !!userId,
    staleTime: Infinity,
  })
  return query.data?.user.display_name || query.data?.user.username || userId || ''
}

export interface UserProfile {
  displayName: string
  avatarColor: string
  avatarUrl?: string
  isBot: boolean
}

/** Resolves a user's live profile (name/avatar) from the shared `['user', id]` cache — the
 * same cache `useUserName` and reaction tooltips already read from — instead of whatever
 * snapshot of the author was embedded in a message at fetch time. That embedded snapshot goes
 * stale the moment the author changes their avatar or display name, since old messages already
 * sitting in the `['messages', ...]`/`['thread', ...]` cache never get patched. `fallback` (the
 * message's own embedded `user` object) is used only until the live query resolves, so there's
 * no flash of a default avatar while the (usually already-cached) request is in flight, and a
 * `user.updated` realtime event elsewhere invalidates `['user', id]`, refreshing every mounted
 * consumer of this hook at once. */
export function useUserProfile(userId: string | undefined, fallback?: MessageUser | null): UserProfile {
  const query = useQuery({
    queryKey: ['user', userId],
    queryFn: () => api.getUser(userId!),
    enabled: !!userId,
    staleTime: Infinity,
  })
  const live = query.data?.user
  return {
    displayName: live?.display_name || fallback?.display_name || live?.username || fallback?.username || userId || '',
    avatarColor: live?.avatar_color || fallback?.avatar_color || '#999',
    avatarUrl: live?.avatar_url ?? fallback?.avatar_url,
    isBot: live?.is_bot ?? fallback?.is_bot ?? false,
  }
}
