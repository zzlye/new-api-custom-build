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
import { useTranslation } from 'react-i18next'

import { PageBackground } from '@/components/page-background'
import { Skeleton } from '@/components/ui/skeleton'
import { useAppearance } from '@/hooks/use-appearance'
import { useSystemConfig } from '@/hooks/use-system-config'
import {
  getLoginBackground,
  hasLoginBackground,
} from '@/lib/appearance'
import { cn } from '@/lib/utils'

type AuthLayoutProps = {
  children: React.ReactNode
}

export function AuthLayout({ children }: AuthLayoutProps) {
  const { t } = useTranslation()
  const { systemName, logo, loading } = useSystemConfig()
  const appearance = useAppearance()
  const loginBg = getLoginBackground(appearance)
  const withBg = hasLoginBackground(appearance)

  return (
    <div
      className={cn(
        'relative grid h-svh max-w-none overflow-hidden',
        withBg ? 'bg-transparent' : 'bg-background'
      )}
    >
      {/* 登录/注册页自定义背景 */}
      <PageBackground config={loginBg} overlayOpacity={0.4} />

      <Link
        to='/'
        className='absolute top-4 left-4 z-10 flex items-center gap-2 transition-opacity hover:opacity-80 sm:top-8 sm:left-8'
      >
        <div className='relative h-8 w-8'>
          {loading ? (
            <Skeleton className='absolute inset-0 rounded-full' />
          ) : (
            <img
              src={logo}
              alt={t('Logo')}
              className='h-8 w-8 rounded-full object-cover'
            />
          )}
        </div>
        {loading ? (
          <Skeleton className='h-6 w-24' />
        ) : (
          <h1
            className={cn(
              'text-xl font-medium',
              withBg && 'text-white drop-shadow-sm'
            )}
          >
            {systemName}
          </h1>
        )}
      </Link>
      <div className='relative z-10 container flex items-center pt-16 sm:pt-0'>
        <div className='mx-auto flex w-full flex-col justify-center space-y-2 px-4 py-8 sm:w-[480px] sm:p-8'>
          {/* 有背景时给表单加半透明卡片，保证可读性 */}
          <div
            className={cn(
              withBg &&
                'bg-background/90 border-border/50 rounded-2xl border p-6 shadow-lg backdrop-blur-md sm:p-8'
            )}
          >
            {children}
          </div>
        </div>
      </div>
    </div>
  )
}
