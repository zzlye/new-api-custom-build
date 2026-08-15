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
import { SettingsPage } from '../components/settings-page'
import type { SiteSettings } from '../types'
import {
  SITE_DEFAULT_SECTION,
  getSiteSectionContent,
  getSiteSectionMeta,
} from './section-registry.tsx'

const defaultSiteSettings: SiteSettings = {
  Notice: '',
  SystemName: 'New API',
  Logo: '',
  Footer: '',
  About: '',
  HomePageContent: '',
  ServerAddress: '',
  'legal.user_agreement': '',
  'legal.privacy_policy': '',
  HeaderNavModules: '',
  SidebarModulesAdmin: '',
  'appearance_setting.theme_preset': 'default',
  'appearance_setting.global_bg_type': 'none',
  'appearance_setting.global_bg_color': '#0f172a',
  'appearance_setting.global_bg_media': '',
  'appearance_setting.global_bg_overlay_opacity': 0,
  'appearance_setting.home_bg_type': 'none',
  'appearance_setting.home_bg_color': '#0f172a',
  'appearance_setting.home_bg_media': '',
  'appearance_setting.home_bg_overlay_opacity': 0,
  'appearance_setting.home_sakura': true,
  'appearance_setting.login_bg_type': 'none',
  'appearance_setting.login_bg_color': '#0f172a',
  'appearance_setting.login_bg_media': '',
  'appearance_setting.login_bg_overlay_opacity': 0,
  'appearance_setting.glass_opacity': 0.72,
  'appearance_setting.glass_blur': 16,
  'appearance_setting.glass_border_opacity': 0.35,
  'appearance_setting.glass_shadow_opacity': 0.35,
}

export function SiteSettings() {
  return (
    <SettingsPage
      routePath='/_authenticated/system-settings/site/$section'
      defaultSettings={defaultSiteSettings}
      defaultSection={SITE_DEFAULT_SECTION}
      getSectionContent={getSiteSectionContent}
      getSectionMeta={getSiteSectionMeta}
    />
  )
}
