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
 * - global_bg_*：全站默认背景（包含公开页面、登录页和后台控制台）
 * - home_bg_*：主页背景
 * - login_bg_*：登录/注册等鉴权页背景
 * - *_bg_overlay_opacity：图片和视频背景的黑色遮罩透明度（0 到 1）
 * - glass_*：卡片毛玻璃透明度、模糊、边框和阴影参数
 */
export type AppearanceConfig = {
  theme_preset: ThemePreset
  global_bg_type: BgType
  global_bg_color: string
  global_bg_media: string
  global_bg_overlay_opacity: number
  home_bg_type: BgType
  home_bg_color: string
  home_bg_media: string
  home_bg_overlay_opacity: number
  home_sakura: boolean
  login_bg_type: BgType
  login_bg_color: string
  login_bg_media: string
  login_bg_overlay_opacity: number
  glass_opacity: number
  glass_blur: number
  glass_border_opacity: number
  glass_shadow_opacity: number
}

/** 单页背景配置切片 */
export type PageBackgroundConfig = {
  type: BgType
  color: string
  media: string
}

export const DEFAULT_APPEARANCE: AppearanceConfig = {
  theme_preset: 'default',
  global_bg_type: 'none',
  global_bg_color: '#0f172a',
  global_bg_media: '',
  global_bg_overlay_opacity: 0,
  home_bg_type: 'none',
  home_bg_color: '#0f172a',
  home_bg_media: '',
  home_bg_overlay_opacity: 0,
  home_sakura: true,
  login_bg_type: 'none',
  login_bg_color: '#0f172a',
  login_bg_media: '',
  login_bg_overlay_opacity: 0,
  glass_opacity: 0.72,
  glass_blur: 16,
  glass_border_opacity: 0.35,
  glass_shadow_opacity: 0.35,
}

export type GlassProfile = Pick<
  AppearanceConfig,
  'glass_opacity' | 'glass_border_opacity' | 'glass_shadow_opacity'
>

export const DEFAULT_GLASS_STRENGTH = 70

const GLASS_PROFILE_ANCHORS: ReadonlyArray<{
  strength: number
  profile: GlassProfile
}> = [
  {
    strength: 0,
    profile: {
      glass_opacity: 0.56,
      glass_border_opacity: 0.2,
      glass_shadow_opacity: 0.045,
    },
  },
  {
    // 保留旧版默认参数，让已有站点首次拖动时从当前观感平滑过渡。
    strength: 50,
    profile: {
      glass_opacity: 0.72,
      glass_border_opacity: 0.35,
      glass_shadow_opacity: 0.35,
    },
  },
  {
    // 日常推荐档位兼顾背景可见度与正文对比度。
    strength: DEFAULT_GLASS_STRENGTH,
    profile: {
      glass_opacity: 0.7,
      glass_border_opacity: 0.84,
      glass_shadow_opacity: 0.15,
    },
  },
  {
    strength: 100,
    profile: {
      glass_opacity: 0.76,
      glass_border_opacity: 1,
      glass_shadow_opacity: 0.195,
    },
  },
]

function interpolateGlassValue(start: number, end: number, ratio: number) {
  return Number((start + (end - start) * ratio).toFixed(3))
}

/** 将简化后的强度值转换为兼容旧接口的三项玻璃参数。 */
export function getGlassProfile(strength: number): GlassProfile {
  const normalized = Math.min(100, Math.max(0, strength))
  const upperIndex = GLASS_PROFILE_ANCHORS.findIndex(
    (anchor) => normalized <= anchor.strength
  )
  if (upperIndex <= 0) {
    return { ...GLASS_PROFILE_ANCHORS[0].profile }
  }

  const lower = GLASS_PROFILE_ANCHORS[upperIndex - 1]
  const upper = GLASS_PROFILE_ANCHORS[upperIndex]
  const ratio =
    (normalized - lower.strength) / (upper.strength - lower.strength)

  return {
    glass_opacity: interpolateGlassValue(
      lower.profile.glass_opacity,
      upper.profile.glass_opacity,
      ratio
    ),
    glass_border_opacity: interpolateGlassValue(
      lower.profile.glass_border_opacity,
      upper.profile.glass_border_opacity,
      ratio
    ),
    glass_shadow_opacity: interpolateGlassValue(
      lower.profile.glass_shadow_opacity,
      upper.profile.glass_shadow_opacity,
      ratio
    ),
  }
}

const GLASS_PROFILES_BY_STRENGTH = Array.from({ length: 101 }, (_, strength) =>
  getGlassProfile(strength)
)

/** 从历史三项参数中找出最接近的简化强度值。 */
export function getGlassStrength(profile: GlassProfile): number {
  if (Object.values(profile).some((value) => !Number.isFinite(value))) {
    return DEFAULT_GLASS_STRENGTH
  }

  let closestStrength = DEFAULT_GLASS_STRENGTH
  let closestDistance = Number.POSITIVE_INFINITY
  GLASS_PROFILES_BY_STRENGTH.forEach((candidate, strength) => {
    const opacityDistance =
      (profile.glass_opacity - candidate.glass_opacity) / 0.2
    const borderDistance =
      (profile.glass_border_opacity - candidate.glass_border_opacity) / 0.8
    const shadowDistance =
      (profile.glass_shadow_opacity - candidate.glass_shadow_opacity) / 0.305
    const distance =
      opacityDistance ** 2 + borderDistance ** 2 + shadowDistance ** 2
    if (distance < closestDistance) {
      closestDistance = distance
      closestStrength = strength
    }
  })

  return closestStrength
}

function normalizeBgType(type: string | undefined): BgType {
  const t = (type || 'none') as BgType
  return ['none', 'solid', 'image', 'video'].includes(t) ? t : 'none'
}

/** 将外部配置中的透明度限制在指定范围，避免非法值影响页面渲染。 */
function normalizeRange(
  value: unknown,
  fallback: number,
  min: number,
  max: number
): number {
  const opacity = Number(value)
  if (!Number.isFinite(opacity)) return fallback
  return Math.min(max, Math.max(min, opacity))
}

/** 将背景遮罩透明度限制在 0 到 1。 */
function normalizeOverlayOpacity(value: unknown, fallback: number): number {
  return normalizeRange(value, fallback, 0, 1)
}

/** 将毛玻璃模糊半径限制在 0 到 40 像素。 */
function normalizeGlassBlur(value: unknown, fallback: number): number {
  return normalizeRange(value, fallback, 0, 40)
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

/** 是否启用了全局背景。 */
export function hasGlobalBackground(config: AppearanceConfig): boolean {
  return hasPageBackground({
    type: config.global_bg_type,
    color: config.global_bg_color,
    media: config.global_bg_media,
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

/** 获取全局背景配置切片。 */
export function getGlobalBackground(
  config: AppearanceConfig
): PageBackgroundConfig {
  return {
    type: config.global_bg_type,
    color: config.global_bg_color,
    media: config.global_bg_media,
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

/** 页面专属背景启用时优先使用专属背景，否则回退到全局背景。 */
function getEffectiveBackground(
  specific: PageBackgroundConfig,
  global: PageBackgroundConfig
): PageBackgroundConfig {
  return hasPageBackground(specific) ? specific : global
}

/** 获取主页最终使用的背景。 */
export function getEffectiveHomeBackground(
  config: AppearanceConfig
): PageBackgroundConfig {
  return getEffectiveBackground(
    getHomeBackground(config),
    getGlobalBackground(config)
  )
}

/** 获取登录/注册页最终使用的背景。 */
export function getEffectiveLoginBackground(
  config: AppearanceConfig
): PageBackgroundConfig {
  return getEffectiveBackground(
    getLoginBackground(config),
    getGlobalBackground(config)
  )
}

/** 获取主页最终背景对应的遮罩透明度。 */
export function getEffectiveHomeBackgroundOverlayOpacity(
  config: AppearanceConfig
): number {
  return hasHomeBackground(config)
    ? config.home_bg_overlay_opacity
    : config.global_bg_overlay_opacity
}

/** 获取登录页最终背景对应的遮罩透明度。 */
export function getEffectiveLoginBackgroundOverlayOpacity(
  config: AppearanceConfig
): number {
  return hasLoginBackground(config)
    ? config.login_bg_overlay_opacity
    : config.global_bg_overlay_opacity
}

/** 判断主页或登录页是否存在可见背景。 */
export function hasEffectiveHomeBackground(config: AppearanceConfig): boolean {
  return hasPageBackground(getEffectiveHomeBackground(config))
}

export function hasEffectiveLoginBackground(config: AppearanceConfig): boolean {
  return hasPageBackground(getEffectiveLoginBackground(config))
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
  body.style.setProperty(
    '--appearance-glass-opacity',
    String(config.glass_opacity)
  )
  body.style.setProperty('--appearance-glass-blur', `${config.glass_blur}px`)
  body.style.setProperty(
    '--appearance-glass-border-opacity',
    String(config.glass_border_opacity)
  )
  body.style.setProperty(
    '--appearance-glass-shadow-opacity',
    String(config.glass_shadow_opacity)
  )

  // 从主参数派生控件层级，确保一个强度值即可保持所有玻璃表面协调。
  const controlOpacity = Math.min(
    0.42,
    Math.max(0.18, config.glass_opacity - 0.46)
  )
  const controlBorderOpacity = Math.min(
    0.62,
    Math.max(0.22, config.glass_border_opacity * 0.57)
  )
  const controlHighlightOpacity = Math.min(
    0.36,
    Math.max(0.12, 0.18 + config.glass_shadow_opacity * 0.66)
  )
  const darkControlOpacity = Math.min(
    0.2,
    Math.max(0.08, (config.glass_opacity - 0.5) * 0.5)
  )
  const darkBorderOpacity = Math.min(
    0.4,
    Math.max(0.16, config.glass_border_opacity * 0.29)
  )
  const darkHighlightOpacity = Math.min(
    0.28,
    Math.max(0.1, 0.1 + config.glass_shadow_opacity * 0.4)
  )
  const fixedCellOpacity = Math.min(
    0.99,
    Math.max(0.94, config.glass_opacity + 0.28)
  )
  const subtleOpacity = Math.min(0.2, Math.max(0.1, controlOpacity * 0.5))
  const subtleDarkOpacity = Math.min(
    0.12,
    Math.max(0.05, darkControlOpacity * 0.55)
  )
  const popupOpacity = Math.min(
    0.92,
    Math.max(0.82, config.glass_opacity + 0.12)
  )
  const popupDarkOpacity = Math.min(
    0.9,
    Math.max(0.78, config.glass_opacity + 0.08)
  )

  body.style.setProperty(
    '--appearance-glass-control-opacity',
    `${Math.round(controlOpacity * 100)}%`
  )
  body.style.setProperty(
    '--appearance-glass-control-hover-opacity',
    `${Math.round(Math.min(0.5, controlOpacity + 0.08) * 100)}%`
  )
  body.style.setProperty(
    '--appearance-glass-control-border-opacity',
    `${Math.round(controlBorderOpacity * 100)}%`
  )
  body.style.setProperty(
    '--appearance-glass-control-highlight-opacity',
    `${Math.round(controlHighlightOpacity * 100)}%`
  )
  body.style.setProperty(
    '--appearance-glass-control-dark-opacity',
    `${Math.round(darkControlOpacity * 100)}%`
  )
  body.style.setProperty(
    '--appearance-glass-control-dark-hover-opacity',
    `${Math.round(Math.min(0.28, darkControlOpacity + 0.06) * 100)}%`
  )
  body.style.setProperty(
    '--appearance-glass-control-dark-border-opacity',
    `${Math.round(darkBorderOpacity * 100)}%`
  )
  body.style.setProperty(
    '--appearance-glass-control-dark-highlight-opacity',
    `${Math.round(darkHighlightOpacity * 100)}%`
  )
  body.style.setProperty(
    '--appearance-glass-fixed-cell-opacity',
    `${Math.round(fixedCellOpacity * 100)}%`
  )
  body.style.setProperty(
    '--appearance-glass-subtle-opacity',
    `${Math.round(subtleOpacity * 100)}%`
  )
  body.style.setProperty(
    '--appearance-glass-subtle-dark-opacity',
    `${Math.round(subtleDarkOpacity * 100)}%`
  )
  body.style.setProperty(
    '--appearance-glass-popup-opacity',
    `${Math.round(popupOpacity * 100)}%`
  )
  body.style.setProperty(
    '--appearance-glass-popup-dark-opacity',
    `${Math.round(popupDarkOpacity * 100)}%`
  )
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
    global_bg_type: normalizeBgType(raw?.global_bg_type),
    global_bg_color:
      raw?.global_bg_color?.trim() || DEFAULT_APPEARANCE.global_bg_color,
    global_bg_media: raw?.global_bg_media?.trim() || '',
    global_bg_overlay_opacity: normalizeOverlayOpacity(
      raw?.global_bg_overlay_opacity,
      DEFAULT_APPEARANCE.global_bg_overlay_opacity
    ),
    home_bg_type: normalizeBgType(raw?.home_bg_type),
    home_bg_color:
      raw?.home_bg_color?.trim() || DEFAULT_APPEARANCE.home_bg_color,
    home_bg_media: raw?.home_bg_media?.trim() || '',
    home_bg_overlay_opacity: normalizeOverlayOpacity(
      raw?.home_bg_overlay_opacity,
      DEFAULT_APPEARANCE.home_bg_overlay_opacity
    ),
    home_sakura:
      raw?.home_sakura !== undefined
        ? Boolean(raw.home_sakura && String(raw.home_sakura) !== 'false')
        : DEFAULT_APPEARANCE.home_sakura,
    login_bg_type: normalizeBgType(raw?.login_bg_type),
    login_bg_color:
      raw?.login_bg_color?.trim() || DEFAULT_APPEARANCE.login_bg_color,
    login_bg_media: raw?.login_bg_media?.trim() || '',
    login_bg_overlay_opacity: normalizeOverlayOpacity(
      raw?.login_bg_overlay_opacity,
      DEFAULT_APPEARANCE.login_bg_overlay_opacity
    ),
    glass_opacity: normalizeRange(
      raw?.glass_opacity,
      DEFAULT_APPEARANCE.glass_opacity,
      0,
      1
    ),
    glass_blur: normalizeGlassBlur(
      raw?.glass_blur,
      DEFAULT_APPEARANCE.glass_blur
    ),
    glass_border_opacity: normalizeRange(
      raw?.glass_border_opacity,
      DEFAULT_APPEARANCE.glass_border_opacity,
      0,
      1
    ),
    glass_shadow_opacity: normalizeRange(
      raw?.glass_shadow_opacity,
      DEFAULT_APPEARANCE.glass_shadow_opacity,
      0,
      1
    ),
  }
}
