/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

/** 主页背景类型 */
export type HomeBgType = 'none' | 'solid' | 'image' | 'video'

/** 站点外观配置（由根用户配置，经 /api/status 下发） */
export type AppearanceConfig = {
  home_bg_type: HomeBgType
  home_bg_color: string
  home_bg_media: string
  success_tone: string
}

export const DEFAULT_APPEARANCE: AppearanceConfig = {
  home_bg_type: 'none',
  home_bg_color: '#0f172a',
  home_bg_media: '',
  success_tone: 'default',
}

/** 成功色预设（欢迎回来 toast 等），与后端 allowedSuccessTones 对齐 */
export const SUCCESS_TONES = [
  {
    value: 'default',
    name: 'Default Green',
    color: 'oklch(0.596 0.145 163.225)',
  },
  {
    value: 'emerald',
    name: 'Emerald',
    color: 'oklch(0.5315 0.0694 156.19)',
  },
  {
    value: 'teal',
    name: 'Teal',
    color: 'oklch(0.765 0.177 163.22)',
  },
  {
    value: 'forest',
    name: 'Forest',
    color: 'oklch(0.5276 0.1072 182.22)',
  },
  {
    value: 'blue',
    name: 'Ocean Blue',
    color: 'oklch(0.5461 0.2152 262.88)',
  },
  {
    value: 'rose',
    name: 'Rose',
    color: 'oklch(0.5827 0.2418 12.23)',
  },
  {
    value: 'amber',
    name: 'Amber',
    color: 'oklch(0.681 0.162 75.834)',
  },
] as const

export type SuccessTone = (typeof SUCCESS_TONES)[number]['value']

export function resolveSuccessToneColor(tone: string): string {
  const found = SUCCESS_TONES.find((item) => item.value === tone)
  return found?.color ?? SUCCESS_TONES[0].color
}

/** 将外观配置应用到 document（成功色 CSS 变量） */
export function applyAppearanceToDocument(config: AppearanceConfig): void {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  const success = resolveSuccessToneColor(config.success_tone || 'default')
  root.style.setProperty('--success', success)
  // 同步 data 属性，便于后续扩展
  root.dataset.successTone = config.success_tone || 'default'
}

export function normalizeAppearance(
  raw: Partial<AppearanceConfig> | null | undefined
): AppearanceConfig {
  const type = (raw?.home_bg_type || 'none') as HomeBgType
  const validType: HomeBgType = ['none', 'solid', 'image', 'video'].includes(
    type
  )
    ? type
    : 'none'
  return {
    home_bg_type: validType,
    home_bg_color: raw?.home_bg_color?.trim() || DEFAULT_APPEARANCE.home_bg_color,
    home_bg_media: raw?.home_bg_media?.trim() || '',
    success_tone: raw?.success_tone?.trim() || 'default',
  }
}
