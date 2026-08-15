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

import { useState } from 'react'

import { GameBottomBar } from './game-bottom-bar'
import { GameLanterns } from './game-lanterns'
import { GameMenuList } from './game-menu-list'
import { GameParallaxViewport } from './game-parallax-viewport'
import { GameSakuraCanvas } from './game-sakura-canvas'
import { GameThemeDialog } from './game-theme-dialog'
import { GameTopBar } from './game-top-bar'
import {
  GAME_SCENES,
  type GameScene,
  type ParallaxIntensity,
  type SakuraDensity,
} from './game-types'
import { GameVideoBackground } from './game-video-background'

interface GameHomeViewProps {
  onToggleClassicMode?: () => void
}

/**
 * 第一人称沉浸式游戏主页主视图
 * 整合 3D 摄像机视口、灾厄动态夜樱视频、漫天落樱粒子、二次元 RPG 垂直菜单与 HUD 交互
 */
export function GameHomeView({ onToggleClassicMode }: GameHomeViewProps) {
  // 当前壁纸场景
  const [currentScene, setCurrentScene] = useState<GameScene>(() => {
    try {
      const savedId = localStorage.getItem('newapi_game_scene_id')
      const matched = GAME_SCENES.find((s) => s.id === savedId)
      return matched || GAME_SCENES[0] // 默认加载夜樱境 · 灾厄 (1.mp4 对应视频)
    } catch {
      return GAME_SCENES[0]
    }
  })

  // 3D 摄像机视差灵敏度
  const [parallaxIntensity, setParallaxIntensity] =
    useState<ParallaxIntensity>(() => {
      try {
        const saved = localStorage.getItem('newapi_game_parallax')
        return (saved as ParallaxIntensity) || 'medium'
      } catch {
        return 'medium'
      }
    })

  // 樱花粒子密度
  const [sakuraDensity, setSakuraDensity] = useState<SakuraDensity>(() => {
    try {
      const saved = localStorage.getItem('newapi_game_sakura')
      return (saved as SakuraDensity) || 'medium'
    } catch {
      return 'medium'
    }
  })

  // 主题与参数设置对话框开关
  const [themeDialogOpen, setThemeDialogOpen] = useState(false)

  // 保存场景选择到 localStorage
  const handleSelectScene = (scene: GameScene) => {
    setCurrentScene(scene)
    try {
      localStorage.setItem('newapi_game_scene_id', scene.id)
    } catch {}
  }

  // 保存视差设置
  const handleParallaxChange = (val: ParallaxIntensity) => {
    setParallaxIntensity(val)
    try {
      localStorage.setItem('newapi_game_parallax', val)
    } catch {}
  }

  // 保存樱花粒子设置
  const handleSakuraChange = (val: SakuraDensity) => {
    setSakuraDensity(val)
    try {
      localStorage.setItem('newapi_game_sakura', val)
    } catch {}
  }

  return (
    <div className='game-home-view fixed inset-0 z-0 size-full overflow-hidden bg-black select-none'>
      <GameParallaxViewport intensity={parallaxIntensity}>
        {/* 背景动态视频层 */}
        <GameVideoBackground scene={currentScene} />

        {/* 3D 漫天落樱与灵火粒子层 */}
        <GameSakuraCanvas density={sakuraDensity} />

        {/* 前景神社灯笼与灵韵微光 */}
        <GameLanterns />

        {/* 前台 UI 布局 */}
        <div className='relative z-20 flex size-full flex-col justify-between'>
          {/* 顶部 HUD 工具栏 */}
          <GameTopBar
            currentScene={currentScene}
            onOpenThemeModal={() => setThemeDialogOpen(true)}
            onToggleClassicMode={onToggleClassicMode}
          />

          {/* 左侧垂直主游戏菜单 */}
          <main className='flex flex-1 items-center px-6 md:px-14 lg:px-20'>
            <GameMenuList
              onOpenSettings={() => setThemeDialogOpen(true)}
            />
          </main>

          {/* 底部信息与快速切换栏 */}
          <GameBottomBar
            currentScene={currentScene}
            onOpenThemeModal={() => setThemeDialogOpen(true)}
          />
        </div>

        {/* 场景与个性化参数设置弹窗 */}
        <GameThemeDialog
          open={themeDialogOpen}
          onOpenChange={setThemeDialogOpen}
          currentScene={currentScene}
          onSelectScene={handleSelectScene}
          parallaxIntensity={parallaxIntensity}
          onParallaxChange={handleParallaxChange}
          sakuraDensity={sakuraDensity}
          onSakuraChange={handleSakuraChange}
        />
      </GameParallaxViewport>
    </div>
  )
}
