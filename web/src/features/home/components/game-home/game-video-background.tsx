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

import { useEffect, useRef, useState } from 'react'

import { cn } from '@/lib/utils'

import type { GameScene } from './game-types'

interface GameVideoBackgroundProps {
  scene: GameScene
  className?: string
}

/**
 * 沉浸式动态视频背景组件
 * 包含超流畅视频循环播放、电影级暗角、神社夜景氛围滤镜与 3D 景深反向视差
 */
export function GameVideoBackground({
  scene,
  className,
}: GameVideoBackgroundProps) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const [isVideoLoaded, setIsVideoLoaded] = useState(false)
  const [loadError, setLoadError] = useState(false)

  useEffect(() => {
    setIsVideoLoaded(false)
    setLoadError(false)
    const video = videoRef.current
    if (!video) return

    video.load()
    const playPromise = video.play()
    if (playPromise !== undefined) {
      playPromise
        .then(() => {
          setIsVideoLoaded(true)
        })
        .catch(() => {
          // 浏览器可能在用户未交互前限制自动播放，保持静音重试
          video.muted = true
          video
            .play()
            .then(() => setIsVideoLoaded(true))
            .catch(() => setLoadError(true))
        })
    }
  }, [scene.videoSrc])

  return (
    <div
      className={cn(
        'game-video-bg-container pointer-events-none absolute inset-0 -z-20 overflow-hidden',
        className
      )}
    >
      {/* 3D 摄像机反向视差运动层 */}
      <div
        className='absolute -inset-[12%] h-[124%] w-[124%] transition-transform duration-75 ease-out will-change-transform'
        style={{
          transform:
            'translate3d(var(--cam-bg-x), var(--cam-bg-y), -60px) rotateX(var(--cam-rot-x)) rotateY(var(--cam-rot-y)) scale(1.14)',
          transformOrigin: '50% 50%',
        }}
      >
        <video
          ref={videoRef}
          src={scene.videoSrc}
          autoPlay
          loop
          muted
          playsInline
          onLoadedData={() => setIsVideoLoaded(true)}
          onError={() => setLoadError(true)}
          className={cn(
            'size-full object-cover object-center transition-opacity duration-1000',
            isVideoLoaded ? 'opacity-100' : 'opacity-0'
          )}
        />

        {/* 视频加载中或失败时的美观渐变底图 */}
        {(!isVideoLoaded || loadError) && (
          <div
            className='absolute inset-0 size-full bg-cover bg-center'
            style={{
              background: `radial-gradient(ellipse at 50% 40%, #2e0827 0%, #130418 60%, #08020a 100%)`,
            }}
          />
        )}
      </div>

      {/* 电影级暗角与夜樱境神社氛围光晕遮罩 */}
      <div
        aria-hidden
        className='pointer-events-none absolute inset-0'
        style={{
          background: [
            'radial-gradient(circle at 50% 45%, transparent 20%, rgba(5, 2, 8, 0.45) 70%, rgba(2, 1, 4, 0.82) 100%)',
            'linear-gradient(to bottom, rgba(12, 4, 20, 0.4) 0%, transparent 25%, transparent 75%, rgba(6, 2, 12, 0.6) 100%)',
          ].join(', '),
        }}
      />

      {/* 左下角与右下角红灯笼暖光呼吸光晕 */}
      <div
        aria-hidden
        className='pointer-events-none absolute -bottom-12 -left-12 size-96 rounded-full opacity-35 blur-3xl animate-pulse'
        style={{
          background:
            'radial-gradient(circle, rgba(244, 63, 94, 0.55) 0%, transparent 70%)',
          animationDuration: '4s',
        }}
      />
      <div
        aria-hidden
        className='pointer-events-none absolute -bottom-12 -right-12 size-96 rounded-full opacity-35 blur-3xl animate-pulse'
        style={{
          background:
            'radial-gradient(circle, rgba(236, 72, 153, 0.5) 0%, transparent 70%)',
          animationDuration: '4.8s',
        }}
      />
    </div>
  )
}
