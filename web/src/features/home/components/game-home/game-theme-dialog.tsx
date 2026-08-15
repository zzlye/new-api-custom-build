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

import { Check, Eye, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'

import { gameSound } from './game-sound'
import {
  GAME_SCENES,
  type GameScene,
  type ParallaxIntensity,
  type SakuraDensity,
} from './game-types'

interface GameThemeDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentScene: GameScene
  onSelectScene: (scene: GameScene) => void
  parallaxIntensity: ParallaxIntensity
  onParallaxChange: (intensity: ParallaxIntensity) => void
  sakuraDensity: SakuraDensity
  onSakuraChange: (density: SakuraDensity) => void
}

/**
 * 场景壁纸切换与沉浸感参数调节模态框
 */
export function GameThemeDialog({
  open,
  onOpenChange,
  currentScene,
  onSelectScene,
  parallaxIntensity,
  onParallaxChange,
  sakuraDensity,
  onSakuraChange,
}: GameThemeDialogProps) {
  const { t, i18n } = useTranslation()
  const isZh = i18n.language.startsWith('zh')

  const handleSelectScene = (scene: GameScene) => {
    gameSound.playSwitchSound()
    onSelectScene(scene)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-w-2xl bg-neutral-950/95 backdrop-blur-2xl border border-rose-500/30 text-white shadow-[0_0_50px_rgba(244,63,94,0.25)] rounded-2xl'>
        <DialogHeader>
          <DialogTitle className='text-xl md:text-2xl font-bold tracking-wide text-transparent bg-clip-text bg-gradient-to-r from-rose-200 via-pink-200 to-rose-400'>
            {t('场景主题与沉浸体验设置')}
          </DialogTitle>
          <DialogDescription className='text-neutral-400 text-xs md:text-sm'>
            {t('自定义第一人称视角壁纸、3D视差强度及环境声画参数')}
          </DialogDescription>
        </DialogHeader>

        <div className='flex flex-col gap-6 py-3'>
          {/* 动态场景选择 */}
          <div className='flex flex-col gap-2.5'>
            <Label className='text-xs font-semibold text-rose-300 uppercase tracking-wider flex items-center gap-1.5'>
              <Sparkles className='size-3.5' />
              {t('动态场景壁纸')}
            </Label>

            <div className='grid grid-cols-1 sm:grid-cols-3 gap-3'>
              {GAME_SCENES.map((scene) => {
                const isSelected = scene.id === currentScene.id
                return (
                  <button
                    key={scene.id}
                    type='button'
                    onClick={() => handleSelectScene(scene)}
                    className={cn(
                      'group relative flex flex-col items-start p-3.5 rounded-xl border text-left transition-all duration-300 cursor-pointer outline-none overflow-hidden',
                      isSelected
                        ? 'bg-rose-950/60 border-rose-500 shadow-[0_0_20px_rgba(244,63,94,0.35)] scale-[1.02]'
                        : 'bg-white/5 border-white/10 hover:border-rose-400/50 hover:bg-white/10'
                    )}
                  >
                    {/* 场景名与选择对勾 */}
                    <div className='flex w-full items-center justify-between'>
                      <span className='font-bold text-sm text-white group-hover:text-rose-200'>
                        {isZh ? scene.name : scene.nameEn}
                      </span>
                      {isSelected && (
                        <div className='flex size-5 items-center justify-center rounded-full bg-rose-500 text-white'>
                          <Check className='size-3 stroke-[3]' />
                        </div>
                      )}
                    </div>

                    {/* 场景标签 */}
                    <span className='mt-1 text-[11px] font-medium text-rose-400/90'>
                      {scene.badgeText}
                    </span>

                    {/* 场景简介 */}
                    <p className='mt-2 text-xs text-neutral-400 line-clamp-2'>
                      {scene.description}
                    </p>
                  </button>
                )
              })}
            </div>
          </div>

          {/* 沉浸感与视差调节 */}
          <div className='grid grid-cols-1 sm:grid-cols-2 gap-4 pt-2 border-t border-white/10'>
            {/* 3D 第一人称视差灵敏度 */}
            <div className='flex flex-col gap-2'>
              <Label className='text-xs font-semibold text-rose-300 flex items-center gap-1.5'>
                <Eye className='size-3.5' />
                {t('3D 摄像机视差灵敏度')}
              </Label>
              <div className='grid grid-cols-4 gap-1.5'>
                {(['off', 'low', 'medium', 'high'] as ParallaxIntensity[]).map(
                  (level) => (
                    <button
                      key={level}
                      type='button'
                      onClick={() => {
                        gameSound.playClickSound()
                        onParallaxChange(level)
                      }}
                      className={cn(
                        'py-1.5 text-xs font-medium rounded-lg border transition-all',
                        parallaxIntensity === level
                          ? 'bg-rose-600 text-white border-rose-400 shadow-[0_0_10px_rgba(244,63,94,0.4)]'
                          : 'bg-white/5 border-white/10 text-neutral-300 hover:bg-white/10'
                      )}
                    >
                      {level === 'off'
                        ? t('关闭')
                        : level === 'low'
                          ? t('轻度')
                          : level === 'medium'
                            ? t('标准')
                            : t('高')}
                    </button>
                  )
                )}
              </div>
            </div>

            {/* 樱花与灵火粒子密度 */}
            <div className='flex flex-col gap-2'>
              <Label className='text-xs font-semibold text-rose-300 flex items-center gap-1.5'>
                <Sparkles className='size-3.5' />
                {t('樱花粒子流光密度')}
              </Label>
              <div className='grid grid-cols-4 gap-1.5'>
                {(['off', 'low', 'medium', 'high'] as SakuraDensity[]).map(
                  (density) => (
                    <button
                      key={density}
                      type='button'
                      onClick={() => {
                        gameSound.playClickSound()
                        onSakuraChange(density)
                      }}
                      className={cn(
                        'py-1.5 text-xs font-medium rounded-lg border transition-all',
                        sakuraDensity === density
                          ? 'bg-rose-600 text-white border-rose-400 shadow-[0_0_10px_rgba(244,63,94,0.4)]'
                          : 'bg-white/5 border-white/10 text-neutral-300 hover:bg-white/10'
                      )}
                    >
                      {density === 'off'
                        ? t('关闭')
                        : density === 'low'
                          ? t('轻柔')
                          : density === 'medium'
                            ? t('适中')
                            : t('漫天')}
                    </button>
                  )
                )}
              </div>
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
