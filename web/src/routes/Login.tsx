import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { ApiError, api } from '../lib/api'

export function Login() {
  const [login, setLogin] = useState('')
  const [password, setPassword] = useState('')
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: () => api.login(login, password),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['auth', 'me'] })
      navigate('/', { replace: true })
    },
  })

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    mutation.mutate()
  }

  return (
    <div className="flex h-full items-center justify-center bg-paper">
      <form
        onSubmit={onSubmit}
        className="w-full max-w-sm rounded-lg border border-rule bg-white p-8 shadow-sm"
      >
        <h1 className="mb-1 font-display text-2xl font-semibold text-ink">hivemind</h1>
        <p className="mb-6 text-sm text-ink-2">Sign in to your workspace.</p>

        <label className="mb-3 block text-sm text-ink-2">
          Username or email
          <input
            className="mt-1 w-full rounded border border-rule bg-paper px-3 py-2 text-ink outline-none focus:border-teal"
            value={login}
            onChange={(e) => setLogin(e.target.value)}
            autoFocus
          />
        </label>

        <label className="mb-4 block text-sm text-ink-2">
          Password
          <input
            type="password"
            className="mt-1 w-full rounded border border-rule bg-paper px-3 py-2 text-ink outline-none focus:border-teal"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </label>

        {mutation.isError && (
          <p className="mb-4 text-sm text-red-700">
            {mutation.error instanceof ApiError ? mutation.error.message : 'Something went wrong.'}
          </p>
        )}

        <button
          type="submit"
          disabled={mutation.isPending}
          className="w-full rounded bg-teal px-3 py-2 font-medium text-white transition hover:opacity-90 disabled:opacity-50"
        >
          {mutation.isPending ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
    </div>
  )
}
