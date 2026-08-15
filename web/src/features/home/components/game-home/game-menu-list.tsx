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

import { useNavigate } from '@tanstack/react-router'
import {
  Award,
  BookOpen,
  Boxes,
  LogOut,
  Network,
  Play,
  Settings,
  Sparkles,
  User,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { clearAuthentication } from '@/lib/auth-session'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import { gameSound } from './game-sound'

interface GameMenuListProps {
  className?: string
  onOpenSettings?: () => void
}

interface MenuItem {
  id: string
  titleZh: string
  titleEn: string
  subtitleZh: string
  subtitleEn: string
  icon: React.ComponentType<{ className?: string }>
  to?: string
  action?: 'signout' | 'settings'
  authRequired?: boolean
}

/**
 * 灾厄游戏风左侧主导航菜单
 * 完美还原视频中的二次元 RPG 垂直菜单、清脆剑鸣悬浮音效与华丽流光切刀
 */
export function GameMenuList({
  className,
  onOpenSettings,
}: GameMenuListProps) {
  const { i18n } = useTranslation()
  const isZh = i18n.language.startsWith('zh')
  const { auth } = useAuthStore()
  const isAuthenticated = !!auth.user
  const navigate = useNavigate()
  const [hoveredId, setHoveredId] = useState<string | null>(null)

  const menuItems: MenuItem[] = [
    {
      id: 'singleplayer',
      titleZh: '单人模式',
      titleEn: 'Single Player',
      subtitleZh: '进入控制台 · 体验智能对话',
      subtitleEn: 'Dashboard & AI Chat Experience',
      icon: Play,
      to: isAuthenticated ? '/dashboard' : '/sign-in',
    },
    {
      id: 'multiplayer',
      titleZh: '多人模式',
      titleEn: 'Multiplayer',
      subtitleZh: '渠道网关 · 负载均衡接入',
      subtitleEn: 'Channel Relay & Load Balancing',
      icon: Network,
      to: isAuthenticated ? '/channels' : '/pricing',
    },
    {
      id: 'achievements',
      titleZh: '成　就',
      titleEn: 'Achievements',
      subtitleZh: '用量统计 · 消耗日志与明细',
      subtitleEn: 'Usage Analytics & Audit Logs',
      icon: Award,
      to: isAuthenticated ? '/usage-logs' : '/pricing',
    },
    {
      id: 'workshop',
      titleZh: '创意工坊',
      titleEn: 'Workshop',
      subtitleZh: '令牌分发 · 模型广场与定价',
      subtitleEn: 'API Tokens & Model Marketplace',
      icon: Boxes,
      to: isAuthenticated ? '/keys' : '/pricing',
    },
    {
      id: 'settings',
      titleZh: '设　置',
      titleEn: 'Settings',
      subtitleZh: '偏好设置 · 场景与系统配置',
      subtitleEn: 'Preferences & System Settings',
      icon: Settings,
      action: 'settings',
    },
    {
      id: 'credits',
      titleZh: '制作人员',
      titleEn: 'Credits',
      subtitleZh: '关于项目 · 开发与致谢团队',
      subtitleEn: 'About Project & Contributors',
      icon: BookOpen,
      to: '/about',
    },
    {
      id: 'auth',
      titleZh: isAuthenticated ? '退出登录' : '登录系统',
      titleEn: isAuthenticated ? 'Exit / Sign Out' : 'Sign In / Register',
      subtitleZh: isAuthenticated
        ? '安全注销当前登录账户'
        : '登录现有账户或注册新账户',
      subtitleEn: isAuthenticated
        ? 'Sign out of current session'
        : 'Sign in to existing account',
      icon: isAuthenticated ? LogOut : User,
      action: isAuthenticated ? 'signout' : undefined,
      to: isAuthenticated ? undefined : '/sign-in',
    },
  ]

  const handleMouseEnter = (id: string) => {
    setHoveredId(id)
    gameSound.playHoverSound()
  }

  const handleClick = (item: MenuItem, e: React.MouseEvent) => {
    gameSound.playClickSound()

    if (item.action === 'settings') {
      e.preventDefault()
      if (onOpenSettings) onOpenSettings()
      return
    }

    if (item.action === 'signout') {
      e.preventDefault()
      clearAuthentication()
      return
    }

    if (item.to) {
      navigate({ to: item.to })
    }
  }

  return (
    <nav
      aria-label='Main Game Menu'
      className={cn(
        'game-menu-list relative z-20 flex flex-col items-start gap-1.5 md:gap-2.5',
        className
      )}
      style={{
        transform:
          'translate3d(calc(var(--cam-fg-x) * 1.05), calc(var(--cam-fg-y) * 1.05), 80px)',
        transformOrigin: 'left center',
      }}
    >
      {menuItems.map((item) => {
        const isHovered = hoveredId === item.id
        const title = isZh ? item.titleZh : item.titleEn
        const subtitle = isZh ? item.subtitleZh : item.subtitleEn
        const Icon = item.icon

        return (
          <div
            key={item.id}
            className='group relative'
            onMouseEnter={() => handleMouseEnter(item.id)}
            onMouseLeave={() => setHoveredId(null)}
          >
            <button
              type='button'
              onClick={(e) => handleClick(item, e)}
              className={cn(
                'relative flex items-center gap-3.5 px-5 py-2.5 md:px-7 md:py-3.5 text-left transition-all duration-300 ease-out cursor-pointer rounded-r-2xl outline-none',
                'text-white/85 hover:text-white',
                isHovered
                  ? 'translate-x-3 scale-[1.03] bg-gradient-to-r from-rose-950/70 via-rose-900/40 to-transparent shadow-[0_0_24px_rgba(244,63,94,0.35)]'
                  : 'hover:bg-white/5'
              )}
            >
              {/* 左侧发光刀光/能量竖条 */}
              <div
                className={cn(
                  'absolute left-0 top-1/2 -translate-y-1/2 w-1.5 rounded-full transition-all duration-300',
                  isHovered
                    ? 'h-4/5 bg-gradient-to-b from-rose-400 via-pink-400 to-rose-600 shadow-[0_0_12px_#f43f5e]'
                    : 'h-0 bg-transparent'
                )}
              />

              {/* 游戏风格图标 */}
              <div
                className={cn(
                  'flex size-6 items-center justify-center transition-transform duration-300',
                  isHovered
                    ? 'scale-115 text-rose-400 drop-shadow-[0_0_8px_#f43f5e]'
                    : 'text-white/60'
                )}
              >
                <Icon className='size-5' />
              </div>

              {/* 主标题文字（书法与二次元游戏菜单排版风格） */}
              <div className='flex flex-col items-start'>
                <span
                  className={cn(
                    'text-xl md:text-2xl font-bold tracking-[0.18em] transition-all duration-200',
                    isHovered
                      ? 'text-white drop-shadow-[0_0_16px_rgba(255,255,255,0.85)] translate-x-0.5'
                      : 'text-white/80 drop-shadow-[0_2px_4px_rgba(0,0,0,0.8)]'
                  )}
                  style={{
                    fontFamily:
                      '"Noto Serif SC", "Songti SC", "SimSun", serif',
                  }}
                >
                  {title}
                </span>

                {/* 悬浮展开的副标题解释 */}
                <span
                  className={cn(
                    'text-xs tracking-wider text-rose-200/90 transition-all duration-300 overflow-hidden whitespace-nowrap',
                    isHovered
                      ? 'max-h-6 opacity-100 mt-0.5 drop-shadow-[0_0_6px_rgba(244,63,94,0.6)]'
                      : 'max-h-0 opacity-0'
                  )}
                >
                  {subtitle}
                </span>
              </div>

              {/* 悬浮右侧小光菱 / 樱花微光 */}
              <Sparkles
                className={cn(
                  'size-4 text-rose-300 ml-2 transition-all duration-300 animate-pulse',
                  isHovered ? 'opacity-100 scale-100' : 'opacity-0 scale-50'
                )}
              />
            </button>
          </div>
        )
      })}
    </nav>
  )
}
