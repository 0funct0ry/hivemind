export interface FuzzyMatch<T> {
  item: T
  score: number
}

const WORD_BOUNDARY = /[\s\-_/]/

/**
 * Case-insensitive subsequence fuzzy scorer (sublime/vscode-style). Returns null when
 * `query`'s characters don't all appear in order within `target`. Higher score is better.
 * Hand-rolled rather than an npm fuzzy-search dependency: the palette's data sets
 * (channels/DMs/people) are small, so an O(n*m) per-item scorer is plenty fast and needs
 * no dependency-budget approval.
 */
export function fuzzyScore(query: string, target: string): number | null {
  if (query === '') return 0
  const q = query.toLowerCase()
  const t = target.toLowerCase()

  let qi = 0
  let score = 0
  let consecutive = 0
  let ti = 0

  for (; ti < t.length && qi < q.length; ti++) {
    if (t[ti] !== q[qi]) {
      consecutive = 0
      continue
    }
    qi++
    consecutive++
    score += 1
    score += consecutive * 2 // reward consecutive runs
    if (ti === 0 || WORD_BOUNDARY.test(t[ti - 1])) score += 5 // reward word-boundary starts
  }

  if (qi < q.length) return null // not all query chars matched, in order

  score -= target.length * 0.05 // slight preference for shorter targets
  return score
}

export function fuzzySearch<T>(query: string, items: T[], key: (item: T) => string, limit = 20): FuzzyMatch<T>[] {
  const results: FuzzyMatch<T>[] = []
  for (const item of items) {
    const score = fuzzyScore(query, key(item))
    if (score !== null) results.push({ item, score })
  }
  results.sort((a, b) => b.score - a.score)
  return results.slice(0, limit)
}
