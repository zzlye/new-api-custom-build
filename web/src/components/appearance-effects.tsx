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

import { useAppearance } from '@/hooks/use-appearance'
import { applyAppearanceToDocument } from '@/lib/appearance'

/**
 * 将根用户配置的整站外观应用到 document。
 * 配色通过 data-theme-preset 驱动，毛玻璃参数通过 CSS 变量驱动。
 */
export function AppearanceEffects() {
  const appearance = useAppearance()

  useEffect(() => {
    applyAppearanceToDocument(appearance)
  }, [appearance])

  return null
}
