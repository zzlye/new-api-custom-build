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
import { useAppearance } from '@/hooks/use-appearance'
import { hasHomeBackground } from '@/lib/appearance'
import { cn } from '@/lib/utils'

type HomeBackgroundProps = {
  className?: string
}

/**
 * 主页全屏背景层。
 * 父级 PublicLayout 必须 transparentBg，否则 bg-background 会盖住本层。
 */
export function HomeBackground({ className }: HomeBackgroundProps) {
  const appearance = useAppearance()

  if (!hasHomeBackground(appearance)) {
    return null
  }

  if (appearance.home_bg_type === 'solid') {
    return (
      <div
        aria-hidden
        className={cn('pointer-events-none absolute inset-0 z-0', className)}
        style={{ background: appearance.home_bg_color || '#0f172a' }}
      />
    )
  }

  if (appearance.home_bg_type === 'image' && appearance.home_bg_media) {
    return (
      <div
        aria-hidden
        className={cn(
          'pointer-events-none absolute inset-0 z-0 overflow-hidden',
          className
        )}
      >
        <img
          src={appearance.home_bg_media}
          alt=''
          className='size-full object-cover'
        />
        <div className='absolute inset-0 bg-black/30' />
      </div>
    )
  }

  if (appearance.home_bg_type === 'video' && appearance.home_bg_media) {
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
          src={appearance.home_bg_media}
          autoPlay
          muted
          loop
          playsInline
        />
        <div className='absolute inset-0 bg-black/35' />
      </div>
    )
  }

  return null
}
