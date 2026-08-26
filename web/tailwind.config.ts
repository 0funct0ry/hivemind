import type { Config } from 'tailwindcss'

// Design tokens taken from internal-docs/MOCKUP.html.
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        paper: '#F1F3EF',
        'paper-2': '#E7EBE5',
        'paper-3': '#DCE1D9',
        ink: '#131A17',
        'ink-2': '#4A5551',
        'ink-3': '#7C867F',
        rule: '#D3DACF',
        'rule-soft': '#E2E7DF',
        teal: '#0E6E60',
        'teal-soft': '#DCEBE6',
        pollen: '#D4930B',
        'pollen-soft': '#F6E7C6',
        deep: '#101917',
      },
      fontFamily: {
        display: ['"Bricolage Grotesque"', 'system-ui', 'sans-serif'],
        body: ['"Instrument Sans"', 'system-ui', 'sans-serif'],
        mono: ['"Martian Mono"', 'ui-monospace', 'monospace'],
      },
    },
  },
} satisfies Config
