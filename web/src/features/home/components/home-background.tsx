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
import { lazy, Suspense, useCallback, useState } from 'react'

import { PageBackground } from '@/components/page-background'
import { useAppearance } from '@/hooks/use-appearance'
import {
  getEffectiveHomeBackground,
  getEffectiveHomeBackgroundOverlayOpacity,
} from '@/lib/appearance'

import { HomeSakura } from './home-sakura'

// 仅在主页使用媒体背景时加载 3D 渲染模块，避免普通主页承担额外资源开销。
const WebGLPanoramaCanvas = lazy(async () => {
  const panoramaModule = await import('./webgl-panorama-canvas')
  return { default: panoramaModule.WebGLPanoramaCanvas }
})

type HomeBackgroundProps = {
  className?: string
}

/**
 * 主页 3D 全景球体/图腾柱背景组件
 * 融合 WebGL 3D 曲面环绕与落樱花瓣粒子，实现真实第一人称转动感
 */
export function HomeBackground({ className }: HomeBackgroundProps) {
  const appearance = useAppearance()
  const routeLoading = useRouterState({ select: (state) => state.isLoading })
  const routePath = useRouterState({
    select: (state) => state.location.pathname,
  })
  const background = getEffectiveHomeBackground(appearance)
  const overlayOpacity = getEffectiveHomeBackgroundOverlayOpacity(appearance)
  const panoramaKey = `${background.type}:${background.media}`
  const [unavailablePanoramaKey, setUnavailablePanoramaKey] = useState<
    string | null
  >(null)
  const panoramaEnabled =
    (background.type === 'image' || background.type === 'video') &&
    Boolean(background.media) &&
    unavailablePanoramaKey !== panoramaKey
  const handlePanoramaUnavailable = useCallback(() => {
    setUnavailablePanoramaKey(panoramaKey)
  }, [panoramaKey])

  return (
    <>
      {/* 图片与视频只保留一份球面纹理，初始化失败时再启用平面回退。 */}
      {!panoramaEnabled ? (
        <PageBackground
          config={background}
          className={className}
          overlayOpacity={overlayOpacity}
          suspendVideo={routeLoading}
          videoPlaybackKey={routePath}
        />
      ) : null}
      {panoramaEnabled ? (
        <Suspense fallback={null}>
          <WebGLPanoramaCanvas
            key={panoramaKey}
            media={background.media}
            mediaType={background.type as 'image' | 'video'}
            onUnavailable={handlePanoramaUnavailable}
            overlayOpacity={overlayOpacity}
            suspendVideo={routeLoading}
          />
        </Suspense>
      ) : null}
      <HomeSakura className={className} />
    </>
  )
}
