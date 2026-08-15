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

import { ChevronRight, Sparkles, Terminal } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import { gameSound } from './game-sound'
import type { GameScene } from './game-types'

interface GameBottomBarProps {
  currentScene: GameScene
  className?: string
  onOpenThemeModal?: () => void
}

/**
 * 沉浸式主页底部系统标识与场景交互提示栏
 */
export function GameBottomBar({
  currentScene,
  className,
  onOpenThemeModal,
}: GameBottomBarProps) {
  const { t, i18n } = useTranslation()
  const isZh = i18n.language.startsWith('zh')

  return (
    <footer
      className={cn(
        'game-bottom-bar relative z-30 flex w-full items-end justify-between p-6 md:p-8 select-none pointer-events-none',
        className
      )}
      style={{
        transform:
          'translate3d(calc(var(--cam-fg-x) * 0.9), calc(var(--cam-fg-y) * 0.9), 90px)',
        transformOrigin: 'bottom center',
      }}
    >
      {/* 左下角 tModLoader / 游戏引擎版本信息 */}
      <div className='flex flex-col items-start gap-1 font-mono text-[11px] md:text-xs text-white/55 drop-shadow-[0_2px_4px_rgba(0,0,0,0.9)]'>
        <div className='flex items-center gap-1.5'>
          <Terminal className='size-3 text-rose-400' />
          <span>tModLoader v2024.1.0 · New API Core</span>
        </div>
        <div className='text-white/40'>
          Terraria v1.4.4.9 · AI Relay Gateway v2.0
        </div>
      </div>

      {/* 底部居中：交互式场景切换胶囊 */}
      <div className='absolute bottom-6 md:bottom-8 left-1/2 -translate-x-1/2 pointer-events-auto'>
        <button
          type='button'
          onClick={() => {
            gameSound.playClickSound()
            if (onOpenThemeModal) onOpenThemeModal()
          }}
          onMouseEnter={() => gameSound.playHoverSound()}
          className={cn(
            'group flex items-center gap-2 rounded-full px-5 py-2 text-xs md:text-sm font-medium tracking-wide transition-all duration-300 cursor-pointer outline-none',
            'bg-black/50 hover:bg-rose-950/70 border border-rose-500/30 hover:border-rose-400/70 backdrop-blur-xl',
            'text-rose-100 hover:text-white shadow-[0_0_20px_rgba(0,0,0,0.6)] hover:shadow-[0_0_25px_rgba(244,63,94,0.45)] hover:scale-105'
          )}
        >
          <Sparkles className='size-3.5 text-rose-400 animate-spin' style={{ animationDuration: '6s' }} />
          <span>
            {t('切换背景主题')}:{' '}
            <strong className='text-rose-300 font-semibold'>
              {isZh ? currentScene.name : currentScene.nameEn}
            </strong>
          </span>
          <ChevronRight className='size-3.5 text-rose-400/80 transition-transform group-hover:translate-x-1' />
        </button>
      </div>

      {/* 右下角版权与作者标志 */}
      <div className='hidden sm:flex flex-col items-end text-[11px] text-white/45 drop-shadow-[0_2px_4px_rgba(0,0,0,0.9)]'>
        <span>© 2023-2026 QuantumNous</span>
        <span className='text-[10px] text-white/30'>High Performance AI Proxy</span>
      </div>
    </footer>
  )
}
