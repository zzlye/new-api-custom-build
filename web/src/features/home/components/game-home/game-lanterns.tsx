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

import { cn } from '@/lib/utils'

interface GameLanternsProps {
  className?: string
}

/**
 * 神社祈愿灯笼与灵韵光华前景装饰
 * 配合 3D 第一人称视角，产生极具层次感的前景深沉浸效果
 */
export function GameLanterns({ className }: GameLanternsProps) {
  return (
    <div
      aria-hidden
      className={cn(
        'game-lanterns-layer pointer-events-none absolute inset-0 -z-10 overflow-hidden',
        className
      )}
      style={{
        transform:
          'translate3d(calc(var(--cam-fg-x) * 1.3), calc(var(--cam-fg-y) * 1.3), 60px)',
        transformOrigin: '50% 50%',
      }}
    >
      {/* 左下角神社石灯笼光辉 */}
      <div className='absolute bottom-12 left-8 md:bottom-20 md:left-16 flex flex-col items-center opacity-85'>
        {/* 灯笼核心火光 */}
        <div className='relative size-6'>
          <div className='absolute -inset-4 rounded-full bg-amber-500/30 blur-md animate-pulse' />
          <div
            className='absolute -inset-10 rounded-full bg-rose-500/25 blur-xl animate-pulse'
            style={{ animationDuration: '3.2s' }}
          />
          <div className='size-2 rounded-full bg-amber-200 blur-[1px]' />
        </div>
      </div>

      {/* 右下角祈愿灯笼光辉 */}
      <div className='absolute bottom-16 right-8 md:bottom-24 md:right-20 flex flex-col items-center opacity-85'>
        <div className='relative size-6'>
          <div
            className='absolute -inset-4 rounded-full bg-rose-500/35 blur-md animate-pulse'
            style={{ animationDuration: '2.8s' }}
          />
          <div
            className='absolute -inset-10 rounded-full bg-amber-500/25 blur-xl animate-pulse'
            style={{ animationDuration: '4s' }}
          />
          <div className='size-2 rounded-full bg-rose-100 blur-[1px]' />
        </div>
      </div>

      {/* 顶部祈愿注连绳符纸微光（Torii Shrine Talismans Aura） */}
      <div className='absolute top-0 inset-x-0 flex justify-center opacity-40 pointer-events-none'>
        <div className='h-24 w-96 bg-gradient-to-b from-rose-500/20 via-pink-500/5 to-transparent blur-2xl' />
      </div>
    </div>
  )
}
