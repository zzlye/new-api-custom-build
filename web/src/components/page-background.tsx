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
import { useLayoutEffect, useRef } from 'react'

import { hasPageBackground, type PageBackgroundConfig } from '@/lib/appearance'
import { cn } from '@/lib/utils'

const VIDEO_RESUME_DELAY_MS = 240

type PageBackgroundProps = {
  config: PageBackgroundConfig
  className?: string
  /** 图片和视频背景的黑色遮罩透明度。 */
  overlayOpacity?: number
  /** 页面切换期间暂停视频，降低新页面合成开销。 */
  suspendVideo?: boolean
  /** 值变化时重新等待页面稳定后再播放视频。 */
  videoPlaybackKey?: string
}

/**
 * 通用页面背景层（纯色 / 图片 / 视频）
 * 父级需 relative 且透明底色，内容层建议 relative z-10
 */
export function PageBackground({
  config,
  className,
  overlayOpacity = 0,
  suspendVideo = false,
  videoPlaybackKey,
}: PageBackgroundProps) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const hasVideoBackground = config.type === 'video' && Boolean(config.media)

  useLayoutEffect(() => {
    if (!hasVideoBackground || typeof document === 'undefined') return

    const root = document.documentElement
    const currentCount = Number.parseInt(
      root.dataset.videoBackgroundCount ?? '0',
      10
    )
    const nextCount =
      Number.isFinite(currentCount) && currentCount > 0 ? currentCount + 1 : 1

    // 用计数维护动态背景状态，避免多个页面背景卸载时误清理标记。
    root.dataset.videoBackgroundCount = String(nextCount)
    root.dataset.videoBackground = 'true'

    return () => {
      const activeCount = Number.parseInt(
        root.dataset.videoBackgroundCount ?? '0',
        10
      )
      const remainingCount = Number.isFinite(activeCount)
        ? Math.max(0, activeCount - 1)
        : 0

      if (remainingCount === 0) {
        delete root.dataset.videoBackgroundCount
        delete root.dataset.videoBackground
        return
      }

      root.dataset.videoBackgroundCount = String(remainingCount)
    }
  }, [hasVideoBackground])

  useLayoutEffect(() => {
    const video = videoRef.current
    if (!video || !hasVideoBackground || !config.media) return

    const media = config.media
    // JSX 已经提供媒体源，仅在源不一致时补绑定，避免重复触发加载。
    if (video.getAttribute('src') !== media) {
      video.setAttribute('src', media)
    }

    return () => {
      // 媒体源已由后续渲染替换时，交给浏览器处理新源，避免误清理。
      if (video.getAttribute('src') !== media) return
      video.pause()
      video.removeAttribute('src')
      video.load()
    }
  }, [config.media, hasVideoBackground])

  useLayoutEffect(() => {
    const video = videoRef.current
    if (!video || !hasVideoBackground) return

    let resumeTimer: number | undefined
    const syncPlayback = () => {
      if (resumeTimer !== undefined) {
        window.clearTimeout(resumeTimer)
      }
      video.pause()
      const windowBlurred =
        typeof document.hasFocus === 'function' && !document.hasFocus()
      if (suspendVideo || document.hidden || windowBlurred) return

      const delay = videoPlaybackKey ? VIDEO_RESUME_DELAY_MS : 0
      resumeTimer = window.setTimeout(() => {
        void video.play().catch(() => {
          // 浏览器可能因省电或自动播放策略暂缓背景视频。
        })
      }, delay)
    }

    document.addEventListener('visibilitychange', syncPlayback)
    window.addEventListener('blur', syncPlayback)
    window.addEventListener('focus', syncPlayback)
    syncPlayback()
    return () => {
      if (resumeTimer !== undefined) {
        window.clearTimeout(resumeTimer)
      }
      document.removeEventListener('visibilitychange', syncPlayback)
      window.removeEventListener('blur', syncPlayback)
      window.removeEventListener('focus', syncPlayback)
    }
  }, [config.media, hasVideoBackground, suspendVideo, videoPlaybackKey])

  if (!hasPageBackground(config)) {
    return null
  }

  if (config.type === 'solid') {
    return (
      <div
        aria-hidden
        className={cn('pointer-events-none absolute inset-0 z-0', className)}
        style={{ background: config.color || '#0f172a' }}
      />
    )
  }

  if (config.type === 'image' && config.media) {
    return (
      <div
        aria-hidden
        className={cn(
          'pointer-events-none absolute inset-0 z-0 overflow-hidden',
          className
        )}
      >
        <img src={config.media} alt='' className='size-full object-cover' />
        <div
          className='absolute inset-0 bg-black'
          style={{ opacity: overlayOpacity }}
        />
      </div>
    )
  }

  if (config.type === 'video' && config.media) {
    return (
      <div
        aria-hidden
        className={cn(
          'page-background-layer pointer-events-none absolute inset-0 z-0 overflow-hidden',
          className
        )}
      >
        <video
          ref={videoRef}
          className='page-background-video size-full object-cover'
          src={config.media}
          muted
          loop
          playsInline
          preload='metadata'
          disablePictureInPicture
          disableRemotePlayback
        />
        <div
          className='absolute inset-0 bg-black'
          style={{ opacity: overlayOpacity }}
        />
      </div>
    )
  }

  return null
}
