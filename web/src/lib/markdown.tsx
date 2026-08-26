import { Fragment, type ReactNode } from 'react'

/**
 * Small hand-written renderer for hivemind's markdown subset (bold, italic,
 * inline code, fenced code, blockquote, links, lists, tables, @mentions,
 * #channel links). No raw HTML passthrough — every node is built from React
 * elements, never dangerouslySetInnerHTML, so there is no injection surface
 * to sanitize.
 */

const FENCE_RE = /```([a-z0-9]*)\n([\s\S]*?)```/g
const MENTION_RE = /(^|[^\w@])@(channel|here|[a-z0-9][a-z0-9._-]{0,31})\b/gi
const CHANNEL_RE = /(^|[^\w#])#([a-z0-9][a-z0-9-]{0,31})\b/gi
const INLINE_CODE_RE = /`([^`\n]+)`/g
const LINK_RE = /(https?:\/\/[^\s<>]+)/g

interface RenderOptions {
  currentUsername?: string
}

function CopyButton({ text }: { text: string }) {
  const handleClick = () => {
    void navigator.clipboard?.writeText(text)
  }
  return (
    <button
      type="button"
      onClick={handleClick}
      className="absolute right-2 top-2 rounded bg-paper-2 px-2 py-0.5 font-mono text-[11px] text-ink-2 opacity-0 transition-opacity group-hover:opacity-100 hover:bg-paper-3"
    >
      Copy
    </button>
  )
}

function renderInline(text: string, key: number, opts: RenderOptions): ReactNode {
  // Split on inline code first so bold/italic/mentions never reach inside a code span.
  const codeParts = text.split(INLINE_CODE_RE)
  const nodes: ReactNode[] = []
  codeParts.forEach((part, i) => {
    if (i % 2 === 1) {
      nodes.push(
        <code key={`c${i}`} className="rounded bg-paper-3 px-1 py-0.5 font-mono text-[0.9em]">
          {part}
        </code>,
      )
    } else {
      nodes.push(...renderText(part, i, opts))
    }
  })
  return <Fragment key={key}>{nodes}</Fragment>
}

function renderText(text: string, keyBase: number, opts: RenderOptions): ReactNode[] {
  const tokens: Array<{ start: number; end: number; node: ReactNode }> = []

  for (const m of text.matchAll(MENTION_RE)) {
    const prefix = m[1]
    const name = m[2]
    const start = m.index! + prefix.length
    const end = start + 1 + name.length
    const isSelf = opts.currentUsername && name.toLowerCase() === opts.currentUsername.toLowerCase()
    const isSpecial = name.toLowerCase() === 'channel' || name.toLowerCase() === 'here'
    tokens.push({
      start,
      end,
      node: (
        <span
          key={`m${start}`}
          className={
            'rounded px-1 font-medium ' +
            (isSelf || isSpecial ? 'bg-pollen-soft text-pollen' : 'bg-teal-soft text-teal')
          }
        >
          @{name}
        </span>
      ),
    })
  }
  for (const m of text.matchAll(CHANNEL_RE)) {
    const prefix = m[1]
    const name = m[2]
    const start = m.index! + prefix.length
    const end = start + 1 + name.length
    tokens.push({
      start,
      end,
      node: (
        <span key={`ch${start}`} className="rounded bg-teal-soft px-1 font-medium text-teal">
          #{name}
        </span>
      ),
    })
  }
  for (const m of text.matchAll(LINK_RE)) {
    const start = m.index!
    const end = start + m[0].length
    tokens.push({
      start,
      end,
      node: (
        <a
          key={`l${start}`}
          href={m[0]}
          target="_blank"
          rel="noopener noreferrer"
          className="text-teal underline hover:no-underline"
        >
          {m[0]}
        </a>
      ),
    })
  }

  tokens.sort((a, b) => a.start - b.start)
  const filtered: typeof tokens = []
  let cursor = 0
  for (const t of tokens) {
    if (t.start < cursor) continue
    filtered.push(t)
    cursor = t.end
  }

  const out: ReactNode[] = []
  let pos = 0
  filtered.forEach((t, i) => {
    if (t.start > pos) out.push(<Fragment key={`t${keyBase}-${i}`}>{applyEmphasis(text.slice(pos, t.start))}</Fragment>)
    out.push(t.node)
    pos = t.end
  })
  if (pos < text.length) out.push(<Fragment key={`t${keyBase}-end`}>{applyEmphasis(text.slice(pos))}</Fragment>)
  return out
}

function applyEmphasis(text: string): ReactNode {
  // Bold (**x**) then italic (*x*), non-overlapping, simple single-pass split.
  const parts = text.split(/(\*\*[^*]+\*\*|\*[^*]+\*)/g)
  return parts.map((part, i) => {
    if (part.startsWith('**') && part.endsWith('**')) {
      return <strong key={i}>{part.slice(2, -2)}</strong>
    }
    if (part.startsWith('*') && part.endsWith('*')) {
      return <em key={i}>{part.slice(1, -1)}</em>
    }
    return part
  })
}

export function renderMarkdown(body: string, opts: RenderOptions = {}): ReactNode {
  const blocks: ReactNode[] = []
  let lastIndex = 0
  let key = 0

  for (const m of body.matchAll(FENCE_RE)) {
    if (m.index! > lastIndex) {
      blocks.push(...renderParagraphs(body.slice(lastIndex, m.index!), key, opts))
      key += 1000
    }
    const code = m[2].replace(/\n$/, '')
    blocks.push(
      <pre key={`fence${key++}`} className="group relative overflow-x-auto rounded-md bg-deep p-3 font-mono text-[13px] text-paper">
        <CopyButton text={code} />
        <code>{code}</code>
      </pre>,
    )
    lastIndex = m.index! + m[0].length
  }
  if (lastIndex < body.length) {
    blocks.push(...renderParagraphs(body.slice(lastIndex), key, opts))
  }

  return <>{blocks}</>
}

const TABLE_SEPARATOR_RE = /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/

/** Splits a `| a | b |` row into cell strings, tolerating missing outer pipes. */
function splitTableRow(line: string): string[] {
  let row = line.trim()
  if (row.startsWith('|')) row = row.slice(1)
  if (row.endsWith('|')) row = row.slice(0, -1)
  return row.split('|').map((cell) => cell.trim())
}

function isTableRow(line: string): boolean {
  return line.includes('|') && line.trim().length > 0
}

function renderParagraphs(text: string, keyBase: number, opts: RenderOptions): ReactNode[] {
  const lines = text.split('\n')
  const out: ReactNode[] = []
  let listBuf: string[] = []
  let quoteBuf: string[] = []
  let key = keyBase

  const flushList = () => {
    if (listBuf.length === 0) return
    out.push(
      <ul key={`ul${key++}`} className="list-disc pl-5">
        {listBuf.map((item, i) => (
          <li key={i}>{renderInline(item, i, opts)}</li>
        ))}
      </ul>,
    )
    listBuf = []
  }
  const flushQuote = () => {
    if (quoteBuf.length === 0) return
    out.push(
      <blockquote key={`bq${key++}`} className="border-l-2 border-rule pl-3 text-ink-2">
        {quoteBuf.map((line, i) => (
          <div key={i}>{renderInline(line, i, opts)}</div>
        ))}
      </blockquote>,
    )
    quoteBuf = []
  }

  let i = 0
  while (i < lines.length) {
    const line = lines[i]

    // A table is a header row immediately followed by a `|---|---|` separator.
    if (isTableRow(line) && i + 1 < lines.length && TABLE_SEPARATOR_RE.test(lines[i + 1])) {
      flushList()
      flushQuote()
      const header = splitTableRow(line)
      const bodyRows: string[][] = []
      i += 2
      while (i < lines.length && isTableRow(lines[i]) && !TABLE_SEPARATOR_RE.test(lines[i])) {
        bodyRows.push(splitTableRow(lines[i]))
        i++
      }
      out.push(
        <div key={`tbl${key++}`} className="overflow-x-auto">
          <table className="my-1 border-collapse text-sm">
            <thead>
              <tr>
                {header.map((cell, ci) => (
                  <th
                    key={ci}
                    className="border border-rule bg-paper-2 px-2 py-1 text-left font-display font-semibold text-ink"
                  >
                    {renderInline(cell, ci, opts)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {bodyRows.map((row, ri) => (
                <tr key={ri}>
                  {row.map((cell, ci) => (
                    <td key={ci} className="border border-rule px-2 py-1 align-top text-ink">
                      {renderInline(cell, ci, opts)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>,
      )
      continue
    }

    const listMatch = /^\s*[-*]\s+(.*)$/.exec(line)
    const quoteMatch = /^\s*>\s?(.*)$/.exec(line)
    if (listMatch) {
      flushQuote()
      listBuf.push(listMatch[1])
      i++
      continue
    }
    if (quoteMatch) {
      flushList()
      quoteBuf.push(quoteMatch[1])
      i++
      continue
    }
    flushList()
    flushQuote()
    if (line.length > 0) {
      out.push(<p key={`p${key++}`}>{renderInline(line, key, opts)}</p>)
    }
    i++
  }
  flushList()
  flushQuote()
  return out
}
