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
import { useRouterState } from '@tanstack/react-router'

import { PageBackground } from '@/components/page-background'
import { useAppearance } from '@/hooks/use-appearance'
import { getGlobalBackground, hasGlobalBackground } from '@/lib/appearance'
import { cn } from '@/lib/utils'

type GlobalBackgroundProps = {
  className?: string
}

/** 渲染站点级背景；页面专属背景由页面自身决定是否覆盖它。 */
export function GlobalBackground(props: GlobalBackgroundProps) {
  const appearance = useAppearance()
  const routeLoading = useRouterState({ select: (state) => state.isLoading })
  const routePath = useRouterState({
    select: (state) => state.location.pathname,
  })
  if (!hasGlobalBackground(appearance)) return null

  return (
    <PageBackground
      config={getGlobalBackground(appearance)}
      className={cn('fixed inset-0', props.className)}
      overlayOpacity={appearance.global_bg_overlay_opacity}
      suspendVideo={routeLoading}
      videoPlaybackKey={routePath}
    />
  )
}
