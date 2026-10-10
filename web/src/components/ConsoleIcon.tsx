import type { ReactNode } from 'react'

const icons: Record<string, ReactNode> = {
  'create-link': <><path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9Z" /><path d="M14 3v6h6M9 14h6m-3-3v6" /></>,
  link: <><path d="M10 13a5 5 0 0 0 7.1 0l3-3A5 5 0 0 0 13 2.9l-1.7 1.7M14 11a5 5 0 0 0-7.1 0l-3 3a5 5 0 0 0 7.1 7.1l1.7-1.7" /></>,
  copy: <><rect x="8" y="8" width="12" height="12" rx="2" /><path d="M16 8V4H4v12h4" /></>,
  trash: <path d="M3 6h18M8 6V3h8v3m3 0-1 15H6L5 6m5 4v7m4-7v7" />,
  arrow: <path d="M4 12h16m-6-6 6 6-6 6" />,
  refresh: <path d="M20 10a8 8 0 1 0-2 8M20 3v7h-7" />,
  check: <path d="m5 12 4 4L19 6" />,
  close: <path d="m6 6 12 12M6 18 18 6" />,
  chevron: <path d="m6 9 6 6 6-6" />,
  settings: <><path d="M10 3h4l.5 2.5 2 .9 2.4-.8 2 3.4-1.9 1.7v2.6l1.9 1.7-2 3.4-2.4-.8-2 .9L14 21h-4l-.5-2.5-2-.9-2.4.8-2-3.4L5 13.3v-2.6L3.1 9l2-3.4 2.4.8 2-.9Z" /><circle cx="12" cy="12" r="3" /></>,
  monitor: <><rect x="3" y="3" width="18" height="14" rx="2" /><path d="M8 21h8m-4-4v4M7 12l3-3 3 2 4-4" /></>,
  cpu: <><rect x="6" y="6" width="12" height="12" rx="2" /><rect x="9" y="9" width="6" height="6" rx="1" /><path d="M9 3v3m6-3v3M9 18v3m6-3v3M3 9h3m-3 6h3m12-6h3m-3 6h3" /></>,
  gpu: <><rect x="3" y="6" width="18" height="12" rx="2" /><circle cx="10" cy="12" r="3" /><path d="M17 10v4M3 4v16m4-2v3m4-3v3" /></>,
  memory: <><rect x="3" y="6" width="18" height="11" rx="2" /><path d="M6 17v3m4-3v3m4-3v3m4-3v3M7 10v3m5-3v3m5-3v3" /></>,
  disk: <><ellipse cx="12" cy="5" rx="8" ry="3" /><path d="M4 5v7c0 4 16 4 16 0V5M4 12v7c0 4 16 4 16 0v-7" /></>,
  cube: <><path d="m12 2 9 5v10l-9 5-9-5V7Zm0 10v10M3 7l9 5 9-5" /></>,
  user: <><circle cx="12" cy="7" r="4" /><path d="M4 21v-3a8 8 0 0 1 16 0v3Z" /></>,
  users: <><circle cx="9" cy="7" r="4" /><path d="M2 21v-3a7 7 0 0 1 14 0v3ZM17 4a4 4 0 0 1 0 8m1 3a5 5 0 0 1 4 5" /></>,
  server: <><rect x="3" y="3" width="18" height="7" rx="2" /><rect x="3" y="14" width="18" height="7" rx="2" /><path d="M7 6h.01M7 17h.01m5-11h6m-6 11h6" /></>,
  palette: <><path d="M12 3a9 9 0 1 0 0 18h1a2 2 0 0 0 2-2c0-1-1-1.5-1-2.5a2.5 2.5 0 0 1 2.5-2.5H18a3 3 0 0 0 3-3 9 9 0 0 0-9-8Z" /><circle cx="7.5" cy="10" r=".9" fill="currentColor" stroke="none" /><circle cx="10.5" cy="6.5" r=".9" fill="currentColor" stroke="none" /><circle cx="15" cy="7" r=".9" fill="currentColor" stroke="none" /><circle cx="18" cy="10.5" r=".9" fill="currentColor" stroke="none" /></>,
  clock: <><circle cx="12" cy="12" r="9" /><path d="M12 6v6l4 2" /></>,
  info: <><circle cx="12" cy="12" r="9" /><path d="M12 11v6m0-10h.01" /></>,
  file: <><path d="M14 2H5v20h14V7Zm0 0v6h5M8 12h8m-8 4h8" /></>,
  plus: <><circle cx="12" cy="12" r="9" /><path d="M12 7v10M7 12h10" /></>,
  search: <><circle cx="10" cy="10" r="7" /><path d="m15 15 6 6" /></>,
  list: <path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01" />,
  grid: <><rect x="3" y="3" width="7" height="7" rx="1" /><rect x="14" y="3" width="7" height="7" rx="1" /><rect x="3" y="14" width="7" height="7" rx="1" /><rect x="14" y="14" width="7" height="7" rx="1" /></>,
  play: <path d="m7 3 14 9-14 9Z" />,
  pause: <><path d="M7 4v16M17 4v16" /></>,
  edit: <><path d="m15 4 5 5-11 11-6 1 1-6Zm-1 1 5 5" /></>,
  external: <><path d="M14 3h7v7m0-7L10 14M10 3H3v18h18v-7" /></>,
  more: <><circle cx="4" cy="12" r="1" /><circle cx="12" cy="12" r="1" /><circle cx="20" cy="12" r="1" /></>,
  menu: <path d="M3 6h18M3 12h18M3 18h18" />,
  logout: <><path d="M9 3H3v18h6m4-15 6 6-6 6M7 12h12" /></>,
}

export function ConsoleIcon({ name, size = 20 }: { name: string; size?: number }) {
  return <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{icons[name] || icons.cube}</svg>
}
