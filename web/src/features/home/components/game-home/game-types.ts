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

/**
 * 沉浸式游戏主页场景配置与类型定义
 */

export interface GameScene {
  id: string
  name: string
  nameEn: string
  description: string
  videoSrc: string
  accentColor: string
  badgeText: string
  subtitle: string
  toriiStyle?: 'shrine' | 'fantasy' | 'cyber'
}

export const GAME_SCENES: GameScene[] = [
  {
    id: 'calamity-night',
    name: '夜樱境 · 终焉',
    nameEn: 'Night Sakura · Calamity',
    description: '红发巫女、鸟居红灯笼与漫天落樱的终焉之境',
    videoSrc: '/videos/calamity-night.mp4',
    accentColor: '#f43f5e',
    badgeText: 'Calamity Overhaul v0.4.03',
    subtitle: '终焉降临 · 智能网关',
    toriiStyle: 'shrine',
  },
  {
    id: 'yatsuko',
    name: '亚津子 · 祈愿',
    nameEn: 'Yatsuko · Wish',
    description: '唯美二次元动态壁纸与星光粒子流光',
    videoSrc: '/videos/yatsuko.mp4',
    accentColor: '#ec4899',
    badgeText: 'Yatsuko Edition',
    subtitle: '星辰相伴 · 算力智联',
    toriiStyle: 'fantasy',
  },
  {
    id: 'elf-princess',
    name: '精灵公主 · 星雨',
    nameEn: 'Elf Princess · Starlight',
    description: '120fps 超高清梦幻星雨与森林精灵',
    videoSrc: '/videos/elf-princess.mp4',
    accentColor: '#38bdf8',
    badgeText: 'Princess Starlight 120fps',
    subtitle: '梦幻星境 · 极速分发',
    toriiStyle: 'fantasy',
  },
]

export type ParallaxIntensity = 'off' | 'low' | 'medium' | 'high'
export type SakuraDensity = 'off' | 'low' | 'medium' | 'high'

export interface GameSettings {
  sceneId: string
  parallax: ParallaxIntensity
  sakura: SakuraDensity
  soundEnabled: boolean
  soundVolume: number
  showHud: boolean
}
