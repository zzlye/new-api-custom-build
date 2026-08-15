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

import { Link } from '@tanstack/react-router'
import {
  Activity,
  BookOpen,
  Globe,
  LayoutTemplate,
  Maximize,
  Minimize,
  Music,
  Palette,
  User,
  Volume2,
  VolumeX,
} from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import { gameSound } from './game-sound'
import type { GameScene } from './game-types'

interface GameTopBarProps {
  currentScene: GameScene
  className?: string
  onOpenThemeModal?: () => void
  onToggleClassicMode?: () => void
}

/**
 * 沉浸式主页顶部 HUD 与操作工具栏
 * 包含游戏风主副标题、场景切换、音效控制、全屏、语言切换与用户态导航
 */
export function GameTopBar({
  currentScene,
  className,
  onOpenThemeModal,
  onToggleClassicMode,
}: GameTopBarProps) {
  const { t, i18n } = useTranslation()
  const isZh = i18n.language.startsWith('zh')
  const { auth } = useAuthStore()
  const isAuthenticated = !!auth.user

  const [isMuted, setIsMuted] = useState(gameSound.isMuted())
  const [isBgmPlaying, setIsBgmPlaying] = useState(gameSound.isBgmPlaying())
  const [isFullscreen, setIsFullscreen] = useState(false)

  // 监听全屏状态变化
  useEffect(() => {
    const handleFullscreenChange = () => {
      setIsFullscreen(!!document.fullscreenElement)
    }
    document.addEventListener('fullscreenchange', handleFullscreenChange)
    return () =>
      document.removeEventListener('fullscreenchange', handleFullscreenChange)
  }, [])

  // 切换音效静音
  const handleToggleMute = () => {
    const nextMuted = !isMuted
    gameSound.setMuted(nextMuted)
    setIsMuted(nextMuted)
    if (nextMuted) {
      setIsBgmPlaying(false)
    } else {
      gameSound.playClickSound()
    }
  }

  // 切换治愈背景环境音
  const handleToggleBgm = () => {
    const active = gameSound.toggleAmbientBgm()
    setIsBgmPlaying(active)
    if (active) {
      setIsMuted(false)
      gameSound.setMuted(false)
    }
  }

  // 切换全屏
  const handleToggleFullscreen = () => {
    gameSound.playClickSound()
    if (!document.fullscreenElement) {
      document.documentElement.requestFullscreen().catch(() => {})
    } else {
      document.exitFullscreen().catch(() => {})
    }
  }

  // 切换语言
  const handleLanguageChange = (lang: string) => {
    gameSound.playClickSound()
    i18n.changeLanguage(lang)
  }

  return (
    <header
      className={cn(
        'game-top-bar relative z-30 flex w-full items-start justify-between p-6 md:p-8 select-none',
        className
      )}
      style={{
        transform:
          'translate3d(calc(var(--cam-fg-x) * 0.9), calc(var(--cam-fg-y) * 0.9), 90px)',
        transformOrigin: 'top center',
      }}
    >
      {/* 左上角游戏大标题与二次元神韵印章 */}
      <div className='flex flex-col items-start gap-1'>
        <div className='flex items-center gap-3'>
          {/* 红色神社神纹徽章 */}
          <div className='flex size-8 md:size-9 items-center justify-center rounded-lg bg-gradient-to-br from-rose-500/80 to-rose-950/90 border border-rose-400/40 shadow-[0_0_16px_rgba(244,63,94,0.5)]'>
            <span className='font-serif text-sm font-black text-rose-100'>
              災
            </span>
          </div>

          <div className='flex flex-col'>
            <h1
              className='text-2xl md:text-3xl font-black tracking-widest text-transparent bg-clip-text bg-gradient-to-r from-rose-100 via-pink-200 to-rose-400 drop-shadow-[0_2px_12px_rgba(244,63,94,0.6)]'
              style={{
                fontFamily:
                  '"Noto Serif SC", "Songti SC", "SimSun", serif',
              }}
            >
              {isZh ? currentScene.name : currentScene.nameEn}
            </h1>
          </div>

          {/* 版本标识胶囊 */}
          <span className='hidden sm:inline-flex items-center rounded-full bg-rose-500/20 px-2.5 py-0.5 text-[11px] font-semibold text-rose-300 border border-rose-500/30 backdrop-blur-md shadow-[0_0_8px_rgba(244,63,94,0.3)]'>
            {currentScene.badgeText}
          </span>
        </div>

        {/* 副标题 */}
        <p className='text-xs md:text-sm tracking-wider text-rose-200/80 drop-shadow-[0_1px_4px_rgba(0,0,0,0.8)] ml-1'>
          {isZh ? currentScene.subtitle : currentScene.description}
        </p>
      </div>

      {/* 右上角快捷 HUD 按钮组 */}
      <div className='flex items-center gap-1.5 md:gap-2.5 rounded-2xl bg-black/40 p-1.5 backdrop-blur-xl border border-white/10 shadow-[0_8px_32px_rgba(0,0,0,0.5)]'>
        {/* 模组指南 / 文档 */}
        <Tooltip>
          <TooltipTrigger>
            <Button
              variant='ghost'
              size='icon'
              className='size-9 text-white/80 hover:text-white hover:bg-white/10 rounded-xl transition-all'
              onMouseEnter={() => gameSound.playHoverSound()}
              render={
                <a
                  href='https://github.com/QuantumNous/new-api'
                  target='_blank'
                  rel='noreferrer'
                >
                  <BookOpen className='size-4' />
                  <span className='sr-only'>{t('模组指南 / 文档')}</span>
                </a>
              }
            />
          </TooltipTrigger>
          <TooltipContent side='bottom'>
            {t('模组指南 / 项目文档')}
          </TooltipContent>
        </Tooltip>

        {/* 状态监控 */}
        <Tooltip>
          <TooltipTrigger>
            <Button
              variant='ghost'
              size='icon'
              className='size-9 text-white/80 hover:text-white hover:bg-white/10 rounded-xl transition-all'
              onMouseEnter={() => gameSound.playHoverSound()}
              render={
                <Link to='/system-info'>
                  <Activity className='size-4 text-emerald-400' />
                  <span className='sr-only'>{t('系统监控')}</span>
                </Link>
              }
            />
          </TooltipTrigger>
          <TooltipContent side='bottom'>{t('服务健康与监控')}</TooltipContent>
        </Tooltip>

        {/* 场景壁纸切换 */}
        <Tooltip>
          <TooltipTrigger>
            <Button
              variant='ghost'
              size='icon'
              onClick={() => {
                gameSound.playClickSound()
                if (onOpenThemeModal) onOpenThemeModal()
              }}
              className='size-9 text-white/80 hover:text-rose-300 hover:bg-rose-500/20 rounded-xl transition-all'
              onMouseEnter={() => gameSound.playHoverSound()}
            >
              <Palette className='size-4' />
              <span className='sr-only'>{t('切换动态场景')}</span>
            </Button>
          </TooltipTrigger>
          <TooltipContent side='bottom'>{t('切换动态壁纸场景')}</TooltipContent>
        </Tooltip>

        {/* 空灵环境音 BGM 开关 */}
        <Tooltip>
          <TooltipTrigger>
            <Button
              variant='ghost'
              size='icon'
              onClick={handleToggleBgm}
              className={cn(
                'size-9 rounded-xl transition-all',
                isBgmPlaying
                  ? 'text-rose-400 bg-rose-500/25 shadow-[0_0_12px_rgba(244,63,94,0.4)]'
                  : 'text-white/80 hover:text-white hover:bg-white/10'
              )}
              onMouseEnter={() => gameSound.playHoverSound()}
            >
              <Music className='size-4' />
              <span className='sr-only'>{t('空灵治愈环境音')}</span>
            </Button>
          </TooltipTrigger>
          <TooltipContent side='bottom'>
            {isBgmPlaying ? t('关闭空灵环境音') : t('开启空灵环境音')}
          </TooltipContent>
        </Tooltip>

        {/* 音效开关 */}
        <Tooltip>
          <TooltipTrigger>
            <Button
              variant='ghost'
              size='icon'
              onClick={handleToggleMute}
              className='size-9 text-white/80 hover:text-white hover:bg-white/10 rounded-xl transition-all'
              onMouseEnter={() => gameSound.playHoverSound()}
            >
              {isMuted ? (
                <VolumeX className='size-4 text-rose-400' />
              ) : (
                <Volume2 className='size-4' />
              )}
              <span className='sr-only'>{t('UI音效开关')}</span>
            </Button>
          </TooltipTrigger>
          <TooltipContent side='bottom'>
            {isMuted ? t('开启 UI 音效') : t('静音 UI 音效')}
          </TooltipContent>
        </Tooltip>

        {/* 经典模式 / 沉浸模式切换 */}
        {onToggleClassicMode && (
          <Tooltip>
            <TooltipTrigger>
              <Button
                variant='ghost'
                size='icon'
                onClick={() => {
                  gameSound.playClickSound()
                  onToggleClassicMode()
                }}
                className='size-9 text-white/80 hover:text-white hover:bg-white/10 rounded-xl transition-all'
                onMouseEnter={() => gameSound.playHoverSound()}
              >
                <LayoutTemplate className='size-4' />
                <span className='sr-only'>{t('切换至经典视图')}</span>
              </Button>
            </TooltipTrigger>
            <TooltipContent side='bottom'>{t('切换经典工作台模式')}</TooltipContent>
          </Tooltip>
        )}

        {/* 多语言切换 */}
        <DropdownMenu>
          <DropdownMenuTrigger>
            <Button
              variant='ghost'
              size='icon'
              className='size-9 text-white/80 hover:text-white hover:bg-white/10 rounded-xl transition-all'
              onMouseEnter={() => gameSound.playHoverSound()}
            >
              <Globe className='size-4' />
              <span className='sr-only'>{t('语言')}</span>
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            align='end'
            className='bg-neutral-900/90 backdrop-blur-xl border-white/10 text-white'
          >
            <DropdownMenuItem onClick={() => handleLanguageChange('zh')}>
              简体中文
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => handleLanguageChange('en')}>
              English
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => handleLanguageChange('zh-TW')}>
              繁體中文
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => handleLanguageChange('ja')}>
              日本語
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>

        {/* 全屏切换 */}
        <Tooltip>
          <TooltipTrigger>
            <Button
              variant='ghost'
              size='icon'
              onClick={handleToggleFullscreen}
              className='size-9 text-white/80 hover:text-white hover:bg-white/10 rounded-xl transition-all'
              onMouseEnter={() => gameSound.playHoverSound()}
            >
              {isFullscreen ? (
                <Minimize className='size-4' />
              ) : (
                <Maximize className='size-4' />
              )}
              <span className='sr-only'>{t('全屏模式')}</span>
            </Button>
          </TooltipTrigger>
          <TooltipContent side='bottom'>
            {isFullscreen ? t('退出全屏') : t('进入沉浸全屏')}
          </TooltipContent>
        </Tooltip>

        {/* 用户头像或登录入口 */}
        <div className='ml-1 pl-1 border-l border-white/15'>
          {isAuthenticated ? (
            <Button
              variant='outline'
              size='sm'
              className='h-9 rounded-xl bg-rose-600/80 hover:bg-rose-500 text-white border-rose-400/40 shadow-[0_0_16px_rgba(244,63,94,0.4)] px-3 text-xs font-semibold'
              render={
                <Link to='/dashboard'>
                  <User className='size-3.5 mr-1.5' />
                  {auth.user?.username || t('控制台')}
                </Link>
              }
            />
          ) : (
            <Button
              variant='outline'
              size='sm'
              className='h-9 rounded-xl bg-gradient-to-r from-rose-600 to-pink-600 hover:from-rose-500 hover:to-pink-500 text-white border-rose-400/50 shadow-[0_0_16px_rgba(244,63,94,0.4)] px-3.5 text-xs font-bold'
              render={
                <Link to='/sign-in'>
                  <User className='size-3.5 mr-1.5' />
                  {t('登录 / 注册')}
                </Link>
              }
            />
          )}
        </div>
      </div>
    </header>
  )
}
