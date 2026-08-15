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
import { useEffect, useRef } from 'react'

const PARALLAX_MEDIA_QUERY =
  '(hover: hover) and (pointer: fine) and (prefers-reduced-motion: no-preference)'
const PARALLAX_STYLE_PROPERTIES = [
  '--home-view-rotate-x',
  '--home-view-rotate-y',
  '--home-view-background-x',
  '--home-view-background-y',
] as const
const MAX_DOME_ROTATE_X = 6
const MAX_DOME_ROTATE_Y = 8
const MAX_DOME_OFFSET_X = 32
const MAX_DOME_OFFSET_Y = 20
const PARALLAX_IDLE_DELAY_MS = 260

function clampUnit(value: number): number {
  return Math.min(1, Math.max(-1, value))
}

/** 将平面坐标映射到半球弧线，让中心稳定、边缘视角变化更明显。 */
function projectToDome(value: number): number {
  return Math.asin(clampUnit(value)) / (Math.PI / 2)
}

/** 让默认主页首屏根据桌面鼠标位置产生半球视角。 */
export function useHeroParallax() {
  const surfaceRef = useRef<HTMLElement>(null)

  useEffect(() => {
    const surface = surfaceRef.current
    if (!surface || typeof window.matchMedia !== 'function') return

    const capabilityQuery = window.matchMedia(PARALLAX_MEDIA_QUERY)
    let animationFrame: number | undefined
    let idleTimer: number | undefined
    let pointerX = 0
    let pointerY = 0

    const resetView = () => {
      if (animationFrame !== undefined) {
        window.cancelAnimationFrame(animationFrame)
        animationFrame = undefined
      }
      if (idleTimer !== undefined) {
        window.clearTimeout(idleTimer)
        idleTimer = undefined
      }
      delete surface.dataset.homeParallaxActive
      for (const property of PARALLAX_STYLE_PROPERTIES) {
        surface.style.removeProperty(property)
      }
    }

    const renderView = () => {
      animationFrame = undefined
      if (!capabilityQuery.matches) {
        resetView()
        return
      }

      const bounds = surface.getBoundingClientRect()
      if (bounds.width <= 0 || bounds.height <= 0) return

      const normalizedX = clampUnit(
        (pointerX - bounds.left - bounds.width / 2) / (bounds.width / 2)
      )
      const normalizedY = clampUnit(
        (pointerY - bounds.top - bounds.height / 2) / (bounds.height / 2)
      )
      const domeX = projectToDome(normalizedX)
      const domeY = projectToDome(normalizedY)

      surface.style.setProperty(
        '--home-view-rotate-x',
        `${(-domeY * MAX_DOME_ROTATE_X).toFixed(2)}deg`
      )
      surface.style.setProperty(
        '--home-view-rotate-y',
        `${(domeX * MAX_DOME_ROTATE_Y).toFixed(2)}deg`
      )
      surface.style.setProperty(
        '--home-view-background-x',
        `${(-domeX * MAX_DOME_OFFSET_X).toFixed(2)}px`
      )
      surface.style.setProperty(
        '--home-view-background-y',
        `${(-domeY * MAX_DOME_OFFSET_Y).toFixed(2)}px`
      )
      surface.dataset.homeParallaxActive = 'true'
      if (idleTimer !== undefined) {
        window.clearTimeout(idleTimer)
      }
      // 鼠标停止后及时释放长期合成提示，保留当前视角但降低空闲显卡占用。
      idleTimer = window.setTimeout(() => {
        delete surface.dataset.homeParallaxActive
        idleTimer = undefined
      }, PARALLAX_IDLE_DELAY_MS)
    }

    const handlePointerMove = (event: PointerEvent) => {
      if (!capabilityQuery.matches) return
      pointerX = event.clientX
      pointerY = event.clientY
      if (animationFrame !== undefined) return
      animationFrame = window.requestAnimationFrame(renderView)
    }

    const handleCapabilityChange = () => {
      if (!capabilityQuery.matches) resetView()
    }
    const handleVisibilityChange = () => {
      if (document.hidden) resetView()
    }

    surface.addEventListener('pointermove', handlePointerMove, {
      passive: true,
    })
    surface.addEventListener('pointerleave', resetView)
    capabilityQuery.addEventListener('change', handleCapabilityChange)
    document.addEventListener('visibilitychange', handleVisibilityChange)
    window.addEventListener('blur', resetView)

    return () => {
      resetView()
      surface.removeEventListener('pointermove', handlePointerMove)
      surface.removeEventListener('pointerleave', resetView)
      capabilityQuery.removeEventListener('change', handleCapabilityChange)
      document.removeEventListener('visibilitychange', handleVisibilityChange)
      window.removeEventListener('blur', resetView)
    }
  }, [])

  return surfaceRef
}
