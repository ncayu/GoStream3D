export const themes = [
  { id: 'blue-tech', name: '蓝色', description: '默认配色', colors: ['#f5f7fa', '#ffffff', '#316cf4'] },
  { id: 'deep-ocean', name: '海蓝', description: '浅色页面 · 海蓝强调色', colors: ['#f5f7fa', '#ffffff', '#216ba5'] },
  { id: 'aurora', name: '紫色', description: '浅色页面 · 紫色强调色', colors: ['#f5f7fa', '#ffffff', '#7556bd'] },
  { id: 'neon-cyan', name: '青色', description: '浅色页面 · 青色强调色', colors: ['#f5f7fa', '#ffffff', '#157d88'] },
  { id: 'sunset', name: '橙色', description: '浅色页面 · 橙色强调色', colors: ['#f5f7fa', '#ffffff', '#b96812'] },
  { id: 'rose', name: '玫红', description: '浅色页面 · 玫红强调色', colors: ['#f5f7fa', '#ffffff', '#b43f68'] },
  { id: 'emerald', name: '绿色', description: '浅色页面 · 绿色强调色', colors: ['#f5f7fa', '#ffffff', '#287c48'] },
  { id: 'graphite', name: '灰色', description: '浅色页面 · 灰色强调色', colors: ['#f5f7fa', '#ffffff', '#53657d'] },
  { id: 'gold', name: '暖金', description: '浅色页面 · 暖金强调色', colors: ['#f5f7fa', '#ffffff', '#8d711d'] },
  { id: 'glacier', name: '浅蓝', description: '浅色页面 · 浅蓝强调色', colors: ['#f5f7fa', '#ffffff', '#2177c0'] },
] as const

export type ThemeId = (typeof themes)[number]['id']
export const defaultTheme: ThemeId = 'blue-tech'

export function isThemeId(value: unknown): value is ThemeId {
  return themes.some(theme => theme.id === value)
}
