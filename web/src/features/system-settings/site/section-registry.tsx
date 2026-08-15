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
  DEFAULT_APPEARANCE,
  type AppearanceConfig,
  type HomeBgType,
} from '@/lib/appearance'

import { SystemInfoSection } from '../general/system-info-section'
import {
  parseHeaderNavModules,
  parseSidebarModulesAdmin,
  serializeHeaderNavModules,
  serializeSidebarModulesAdmin,
} from '../maintenance/config'
import { HeaderNavigationSection } from '../maintenance/header-navigation-section'
import { NoticeSection } from '../maintenance/notice-section'
import { SidebarModulesSection } from '../maintenance/sidebar-modules-section'
import type { SiteSettings } from '../types'
import { createSectionRegistry } from '../utils/section-registry'
import { AppearanceSection } from './appearance-section'

function parseBgType(
  value: string | undefined,
  fallback: HomeBgType
): HomeBgType {
  const t = (value || fallback) as HomeBgType
  return ['none', 'solid', 'image', 'video'].includes(t) ? t : 'none'
}

/** 将系统设置中的透明度转换为可供滑块使用的 0 到 1 数字。 */
function parseOverlayOpacity(value: number | undefined): number {
  return typeof value === 'number' && Number.isFinite(value)
    ? Math.min(1, Math.max(0, value))
    : 0
}

function buildAppearanceDefaults(settings: SiteSettings): AppearanceConfig {
  const preset =
    settings['appearance_setting.theme_preset'] ||
    DEFAULT_APPEARANCE.theme_preset
  return {
    theme_preset: preset as AppearanceConfig['theme_preset'],
    global_bg_type: parseBgType(
      settings['appearance_setting.global_bg_type'],
      DEFAULT_APPEARANCE.global_bg_type
    ),
    global_bg_color:
      settings['appearance_setting.global_bg_color'] ||
      DEFAULT_APPEARANCE.global_bg_color,
    global_bg_media: settings['appearance_setting.global_bg_media'] || '',
    global_bg_overlay_opacity: parseOverlayOpacity(
      settings['appearance_setting.global_bg_overlay_opacity']
    ),
    home_bg_type: parseBgType(
      settings['appearance_setting.home_bg_type'],
      DEFAULT_APPEARANCE.home_bg_type
    ),
    home_bg_color:
      settings['appearance_setting.home_bg_color'] ||
      DEFAULT_APPEARANCE.home_bg_color,
    home_bg_media: settings['appearance_setting.home_bg_media'] || '',
    home_bg_overlay_opacity: parseOverlayOpacity(
      settings['appearance_setting.home_bg_overlay_opacity']
    ),
    home_sakura:
      settings['appearance_setting.home_sakura'] ??
      DEFAULT_APPEARANCE.home_sakura,
    login_bg_type: parseBgType(
      settings['appearance_setting.login_bg_type'],
      DEFAULT_APPEARANCE.login_bg_type
    ),
    login_bg_color:
      settings['appearance_setting.login_bg_color'] ||
      DEFAULT_APPEARANCE.login_bg_color,
    login_bg_media: settings['appearance_setting.login_bg_media'] || '',
    login_bg_overlay_opacity: parseOverlayOpacity(
      settings['appearance_setting.login_bg_overlay_opacity']
    ),
    glass_opacity: parseAppearanceRange(
      settings['appearance_setting.glass_opacity'],
      DEFAULT_APPEARANCE.glass_opacity,
      0,
      1
    ),
    glass_blur: parseAppearanceRange(
      settings['appearance_setting.glass_blur'],
      DEFAULT_APPEARANCE.glass_blur,
      0,
      40
    ),
    glass_border_opacity: parseAppearanceRange(
      settings['appearance_setting.glass_border_opacity'],
      DEFAULT_APPEARANCE.glass_border_opacity,
      0,
      1
    ),
    glass_shadow_opacity: parseAppearanceRange(
      settings['appearance_setting.glass_shadow_opacity'],
      DEFAULT_APPEARANCE.glass_shadow_opacity,
      0,
      1
    ),
  }
}

/** 将设置接口返回的数值限制在指定范围，并兼容字符串格式。 */
function parseAppearanceRange(
  value: number | string | undefined,
  fallback: number,
  min: number,
  max: number
): number {
  const parsed = Number(value)
  if (!Number.isFinite(parsed)) return fallback
  return Math.min(max, Math.max(min, parsed))
}

const SITE_SECTIONS = [
  {
    id: 'system-info',
    titleKey: 'System Information',
    build: (settings: SiteSettings) => (
      <SystemInfoSection
        defaultValues={{
          SystemName: settings.SystemName,
          Logo: settings.Logo,
          Footer: settings.Footer,
          About: settings.About,
          HomePageContent: settings.HomePageContent,
          ServerAddress: settings.ServerAddress,
          legal: {
            user_agreement: settings['legal.user_agreement'],
            privacy_policy: settings['legal.privacy_policy'],
          },
        }}
      />
    ),
  },
  {
    id: 'appearance',
    titleKey: 'Appearance',
    build: (settings: SiteSettings) => (
      <AppearanceSection defaultValues={buildAppearanceDefaults(settings)} />
    ),
  },
  {
    id: 'notice',
    titleKey: 'System Notice',
    build: (settings: SiteSettings) => (
      <NoticeSection defaultValue={settings.Notice ?? ''} />
    ),
  },
  {
    id: 'header-navigation',
    titleKey: 'Header navigation',
    build: (settings: SiteSettings) => {
      const headerNavConfig = parseHeaderNavModules(settings.HeaderNavModules)
      const headerNavSerialized = serializeHeaderNavModules(headerNavConfig)
      return (
        <HeaderNavigationSection
          config={headerNavConfig}
          initialSerialized={headerNavSerialized}
        />
      )
    },
  },
  {
    id: 'sidebar-modules',
    titleKey: 'Sidebar modules',
    build: (settings: SiteSettings) => {
      const sidebarConfig = parseSidebarModulesAdmin(
        settings.SidebarModulesAdmin
      )
      const sidebarSerialized = serializeSidebarModulesAdmin(sidebarConfig)
      return (
        <SidebarModulesSection
          config={sidebarConfig}
          initialSerialized={sidebarSerialized}
        />
      )
    },
  },
] as const

export type SiteSectionId = (typeof SITE_SECTIONS)[number]['id']

const siteRegistry = createSectionRegistry<SiteSectionId, SiteSettings>({
  sections: SITE_SECTIONS,
  defaultSection: 'system-info',
  basePath: '/system-settings/site',
  urlStyle: 'path',
})

export const SITE_SECTION_IDS = siteRegistry.sectionIds
export const SITE_DEFAULT_SECTION = siteRegistry.defaultSection
export const getSiteSectionNavItems = siteRegistry.getSectionNavItems
export const getSiteSectionContent = siteRegistry.getSectionContent
export const getSiteSectionMeta = siteRegistry.getSectionMeta
