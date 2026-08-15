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
  '--home-view-foreground-x',
  '--home-view-foreground-y',
] as const

function clampUnit(value: number): number {
  return Math.min(1, Math.max(-1, value))
}

/** 让默认主页首屏根据桌面鼠标位置产生轻微的分层透视。 */
export function useHeroParallax() {
  const surfaceRef = useRef<HTMLElement>(null)

  useEffect(() => {
    const surface = surfaceRef.current
    if (!surface || typeof window.matchMedia !== 'function') return

    const capabilityQuery = window.matchMedia(PARALLAX_MEDIA_QUERY)
    let animationFrame: number | undefined
    let pointerX = 0
    let pointerY = 0

    const resetView = () => {
      if (animationFrame !== undefined) {
        window.cancelAnimationFrame(animationFrame)
        animationFrame = undefined
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

      surface.style.setProperty(
        '--home-view-rotate-x',
        `${(-normalizedY * 3.2).toFixed(2)}deg`
      )
      surface.style.setProperty(
        '--home-view-rotate-y',
        `${(normalizedX * 4.2).toFixed(2)}deg`
      )
      surface.style.setProperty(
        '--home-view-background-x',
        `${(-normalizedX * 16).toFixed(2)}px`
      )
      surface.style.setProperty(
        '--home-view-background-y',
        `${(-normalizedY * 10).toFixed(2)}px`
      )
      surface.style.setProperty(
        '--home-view-foreground-x',
        `${(normalizedX * 7).toFixed(2)}px`
      )
      surface.style.setProperty(
        '--home-view-foreground-y',
        `${(normalizedY * 5).toFixed(2)}px`
      )
      surface.dataset.homeParallaxActive = 'true'
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
