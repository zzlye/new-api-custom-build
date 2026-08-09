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
import { hasPageBackground, type PageBackgroundConfig } from '@/lib/appearance'
import { cn } from '@/lib/utils'

type PageBackgroundProps = {
  config: PageBackgroundConfig
  className?: string
  /** 图片和视频背景的黑色遮罩透明度。 */
  overlayOpacity?: number
}

/**
 * 通用页面背景层（纯色 / 图片 / 视频）
 * 父级需 relative 且透明底色，内容层建议 relative z-10
 */
export function PageBackground({
  config,
  className,
  overlayOpacity = 0,
}: PageBackgroundProps) {
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
          'pointer-events-none absolute inset-0 z-0 overflow-hidden',
          className
        )}
      >
        <video
          className='size-full object-cover'
          src={config.media}
          autoPlay
          muted
          loop
          playsInline
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
