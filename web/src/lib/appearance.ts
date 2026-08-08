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
import {
  THEME_PRESET_VALUES,
  type ThemePreset,
} from '@/lib/theme-customization'

/** 主页背景类型 */
export type HomeBgType = 'none' | 'solid' | 'image' | 'video'

/**
 * 站点外观（根用户配置，全站生效）
 * - theme_preset：整站配色（主色、成功、警告、侧边栏等一整套）
 * - home_bg_*：仅主页背景
 */
export type AppearanceConfig = {
  theme_preset: ThemePreset
  home_bg_type: HomeBgType
  home_bg_color: string
  home_bg_media: string
}

export const DEFAULT_APPEARANCE: AppearanceConfig = {
  theme_preset: 'default',
  home_bg_type: 'none',
  home_bg_color: '#0f172a',
  home_bg_media: '',
}

/** 是否启用了自定义主页背景（需要布局透明才能透出） */
export function hasHomeBackground(config: AppearanceConfig): boolean {
  if (config.home_bg_type === 'none') return false
  if (config.home_bg_type === 'solid') return true
  return Boolean(config.home_bg_media)
}

/**
 * 应用整站外观到 document：
 * - 配色方案写入 body[data-theme-preset]，驱动 theme-presets.css 中全部语义色
 * - 成功/警告/主色等由预设统一控制，不再单独拆「欢迎回来色」
 */
export function applyAppearanceToDocument(config: AppearanceConfig): void {
  if (typeof document === 'undefined') return
  const body = document.body
  if (!body) return

  const preset = config.theme_preset || 'default'
  if (preset === 'default') {
    // default 预设使用 theme.css 根变量，去掉 data 属性即可
    body.removeAttribute('data-theme-preset')
  } else {
    body.setAttribute('data-theme-preset', preset)
  }
  body.dataset.siteThemePreset = preset
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

  const presetRaw = (raw?.theme_preset || 'default') as string
  const theme_preset = THEME_PRESET_VALUES.has(presetRaw as ThemePreset)
    ? (presetRaw as ThemePreset)
    : 'default'

  return {
    theme_preset,
    home_bg_type: validType,
    home_bg_color:
      raw?.home_bg_color?.trim() || DEFAULT_APPEARANCE.home_bg_color,
    home_bg_media: raw?.home_bg_media?.trim() || '',
  }
}
