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
import { useEffect } from 'react'

import { useStatus } from '@/hooks/use-status'
import {
  applyAppearanceToDocument,
  normalizeAppearance,
  type AppearanceConfig,
} from '@/lib/appearance'

/**
 * 监听 /api/status 中的外观配置，将成功色等全局样式应用到 document。
 * 仅根用户可改配置；所有访客/用户共享同一套展示效果。
 */
export function AppearanceEffects() {
  const { status } = useStatus()

  useEffect(() => {
    // getStatus 已解包 data，status 即为状态对象
    const raw = status?.appearance as Partial<AppearanceConfig> | undefined
    applyAppearanceToDocument(normalizeAppearance(raw))
  }, [status])

  return null
}
