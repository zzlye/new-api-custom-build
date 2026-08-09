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

/** 页面背景类型 */
export type BgType = 'none' | 'solid' | 'image' | 'video'
/** @deprecated 使用 BgType */
export type HomeBgType = BgType

/**
 * 站点外观（根用户配置，全站生效）
 * - theme_preset：整站配色
 * - home_bg_*：主页背景
 * - login_bg_*：登录/注册等鉴权页背景
 * - *_bg_overlay_opacity：图片和视频背景的黑色遮罩透明度（0 到 1）
 */
export type AppearanceConfig = {
  theme_preset: ThemePreset
  home_bg_type: BgType
  home_bg_color: string
  home_bg_media: string
  home_bg_overlay_opacity: number
  login_bg_type: BgType
  login_bg_color: string
  login_bg_media: string
  login_bg_overlay_opacity: number
}

/** 单页背景配置切片 */
export type PageBackgroundConfig = {
  type: BgType
  color: string
  media: string
}

export const DEFAULT_APPEARANCE: AppearanceConfig = {
  theme_preset: 'default',
  home_bg_type: 'none',
  home_bg_color: '#0f172a',
  home_bg_media: '',
  home_bg_overlay_opacity: 0,
  login_bg_type: 'none',
  login_bg_color: '#0f172a',
  login_bg_media: '',
  login_bg_overlay_opacity: 0,
}

function normalizeBgType(type: string | undefined): BgType {
  const t = (type || 'none') as BgType
  return ['none', 'solid', 'image', 'video'].includes(t) ? t : 'none'
}

/** 将外部配置中的遮罩透明度限制在 0 到 1，避免非法值影响页面渲染。 */
function normalizeOverlayOpacity(value: unknown, fallback: number): number {
  const opacity = Number(value)
  if (!Number.isFinite(opacity)) return fallback
  return Math.min(1, Math.max(0, opacity))
}

/** 是否启用了自定义背景 */
export function hasPageBackground(bg: PageBackgroundConfig): boolean {
  if (bg.type === 'none') return false
  if (bg.type === 'solid') return true
  return Boolean(bg.media)
}

export function hasHomeBackground(config: AppearanceConfig): boolean {
  return hasPageBackground({
    type: config.home_bg_type,
    color: config.home_bg_color,
    media: config.home_bg_media,
  })
}

export function hasLoginBackground(config: AppearanceConfig): boolean {
  return hasPageBackground({
    type: config.login_bg_type,
    color: config.login_bg_color,
    media: config.login_bg_media,
  })
}

export function getHomeBackground(
  config: AppearanceConfig
): PageBackgroundConfig {
  return {
    type: config.home_bg_type,
    color: config.home_bg_color,
    media: config.home_bg_media,
  }
}

export function getLoginBackground(
  config: AppearanceConfig
): PageBackgroundConfig {
  return {
    type: config.login_bg_type,
    color: config.login_bg_color,
    media: config.login_bg_media,
  }
}

/**
 * 应用整站外观到 document（配色方案）
 */
export function applyAppearanceToDocument(config: AppearanceConfig): void {
  if (typeof document === 'undefined') return
  const body = document.body
  if (!body) return

  const preset = config.theme_preset || 'default'
  if (preset === 'default') {
    body.removeAttribute('data-theme-preset')
  } else {
    body.setAttribute('data-theme-preset', preset)
  }
  body.dataset.siteThemePreset = preset
}

export function normalizeAppearance(
  raw: Partial<AppearanceConfig> | null | undefined
): AppearanceConfig {
  const presetRaw = (raw?.theme_preset || 'default') as string
  const theme_preset = THEME_PRESET_VALUES.has(presetRaw as ThemePreset)
    ? (presetRaw as ThemePreset)
    : 'default'

  return {
    theme_preset,
    home_bg_type: normalizeBgType(raw?.home_bg_type),
    home_bg_color:
      raw?.home_bg_color?.trim() || DEFAULT_APPEARANCE.home_bg_color,
    home_bg_media: raw?.home_bg_media?.trim() || '',
    home_bg_overlay_opacity: normalizeOverlayOpacity(
      raw?.home_bg_overlay_opacity,
      DEFAULT_APPEARANCE.home_bg_overlay_opacity
    ),
    login_bg_type: normalizeBgType(raw?.login_bg_type),
    login_bg_color:
      raw?.login_bg_color?.trim() || DEFAULT_APPEARANCE.login_bg_color,
    login_bg_media: raw?.login_bg_media?.trim() || '',
    login_bg_overlay_opacity: normalizeOverlayOpacity(
      raw?.login_bg_overlay_opacity,
      DEFAULT_APPEARANCE.login_bg_overlay_opacity
    ),
  }
}
