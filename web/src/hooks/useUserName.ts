import { useQuery } from '@tanstack/react-query'
import { api } from '../lib/api'

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
