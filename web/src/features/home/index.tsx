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
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { RichContent } from '@/components/rich-content'
import { useTheme } from '@/context/theme-provider'
import { useAppearance } from '@/hooks/use-appearance'
import { hasEffectiveHomeBackground } from '@/lib/appearance'
import { isLikelyHtml } from '@/lib/content-format'
import { useAuthStore } from '@/stores/auth-store'

import { GameHomeView, Hero } from './components'
import { HomeBackground } from './components/home-background'
import { useHomePageContent } from './hooks'

export function Home() {
  const { i18n, t } = useTranslation()
  const iframeRef = useRef<HTMLIFrameElement>(null)
  const { resolvedTheme } = useTheme()
  const { auth } = useAuthStore()
  const isAuthenticated = !!auth.user
  const { content, isLoaded, isUrl } = useHomePageContent()
  const appearance = useAppearance()

  // 主页展示模式：默认沉浸式第一人称游戏主页 ('immersive')，可切换为经典工作台 ('classic')
  const [viewMode, setViewMode] = useState<'immersive' | 'classic'>(() => {
    try {
      const saved = localStorage.getItem('newapi_home_view_mode')
      return saved === 'classic' ? 'classic' : 'immersive'
    } catch {
      return 'immersive'
    }
  })

  const toggleViewMode = () => {
    const next = viewMode === 'immersive' ? 'classic' : 'immersive'
    setViewMode(next)
    try {
      localStorage.setItem('newapi_home_view_mode', next)
    } catch {}
  }

  // 有页面或全局背景时布局必须透明，否则不透明底色会盖住背景媒体
  const transparentBg = hasEffectiveHomeBackground(appearance)

  const syncIframePreferences = useCallback(() => {
    try {
      iframeRef.current?.contentWindow?.postMessage(
        { themeMode: resolvedTheme },
        '*'
      )
      iframeRef.current?.contentWindow?.postMessage(
        { lang: i18n.language },
        '*'
      )
    } catch {
      // Cross-origin frames may reject access while navigating.
    }
  }, [i18n.language, resolvedTheme])

  useEffect(() => {
    if (isUrl) {
      syncIframePreferences()
    }
  }, [isUrl, syncIframePreferences])

  if (!isLoaded) {
    return (
      <PublicLayout
        showMainContainer={false}
        transparentBg={transparentBg}
        disableGlobalBackground
      >
        <HomeBackground />
        <main className='flex min-h-screen items-center justify-center'>
          <div className='text-muted-foreground'>{t('Loading...')}</div>
        </main>
      </PublicLayout>
    )
  }

  if (content) {
    if (isUrl) {
      return (
        <PublicLayout
          showMainContainer={false}
          transparentBg={transparentBg}
          disableGlobalBackground
        >
          <HomeBackground />
          <iframe
            ref={iframeRef}
            src={content}
            className='h-screen w-full border-none'
            title={t('Custom Home Page')}
            sandbox='allow-forms allow-popups allow-popups-to-escape-sandbox allow-scripts allow-top-navigation-by-user-activation'
            onLoad={syncIframePreferences}
          />
        </PublicLayout>
      )
    }

    const contentIsHtml = isLikelyHtml(content)

    if (contentIsHtml) {
      return (
        <PublicLayout
          showMainContainer={false}
          transparentBg={transparentBg}
          disableGlobalBackground
        >
          <HomeBackground />
          <RichContent
            mode='html'
            htmlVariant='isolated'
            content={content}
            className='custom-home-content'
          />
        </PublicLayout>
      )
    }

    return (
      <PublicLayout transparentBg={transparentBg} disableGlobalBackground>
        <HomeBackground />
        <div className='mx-auto max-w-6xl px-4 py-8'>
          <RichContent
            mode='markdown'
            content={content}
            className='custom-home-content'
          />
        </div>
      </PublicLayout>
    )
  }

  // 沉浸式第一人称视角模式（默认展示）
  if (viewMode === 'immersive') {
    return <GameHomeView onToggleClassicMode={toggleViewMode} />
  }

  // 经典工作台模式
  return (
    <PublicLayout
      showMainContainer={false}
      transparentBg={transparentBg}
      disableGlobalBackground
    >
      <Hero
        isAuthenticated={isAuthenticated}
        onToggleImmersiveMode={toggleViewMode}
      />
    </PublicLayout>
  )
}


