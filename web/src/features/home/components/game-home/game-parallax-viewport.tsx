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

import React, { useEffect, useRef } from 'react'

import { cn } from '@/lib/utils'

import type { ParallaxIntensity } from './game-types'

interface GameParallaxViewportProps {
  children: React.ReactNode
  intensity?: ParallaxIntensity
  className?: string
  onMouseMoveCoords?: (normX: number, normY: number, speed: number) => void
}

/**
 * 第一人称 3D 环顾视差视口组件
 * 模拟真实 3D 摄像机环顾视角，将鼠标或陀螺仪映射为多层景深偏转
 */
export function GameParallaxViewport({
  children,
  intensity = 'medium',
  className,
  onMouseMoveCoords,
}: GameParallaxViewportProps) {
  const containerRef = useRef<HTMLDivElement>(null)

  // 灵敏度倍率配置
  const getMultiplier = () => {
    switch (intensity) {
      case 'off':
        return 0
      case 'low':
        return 0.55
      case 'high':
        return 1.45
      case 'medium':
      default:
        return 1.0
    }
  }

  useEffect(() => {
    const el = containerRef.current
    if (!el || intensity === 'off') return

    let rafId: number | undefined
    let targetX = 0
    let targetY = 0
    let currentX = 0
    let currentY = 0
    let lastTime = performance.now()
    let lastMouseX = 0
    let lastMouseY = 0

    const mult = getMultiplier()
    const maxRotX = 11 * mult
    const maxRotY = 16 * mult
    const maxBgOffsetX = 36 * mult
    const maxBgOffsetY = 24 * mult
    const maxFgOffsetX = 18 * mult
    const maxFgOffsetY = 12 * mult

    // 物理平滑阻尼更新循环
    const updateLoop = () => {
      // 线性插值平滑 Lerp
      const lerpFactor = 0.075
      currentX += (targetX - currentX) * lerpFactor
      currentY += (targetY - currentY) * lerpFactor

      const rotX = -currentY * maxRotX
      const rotY = currentX * maxRotY
      const bgX = -currentX * maxBgOffsetX
      const bgY = -currentY * maxBgOffsetY
      const fgX = currentX * maxFgOffsetX
      const fgY = currentY * maxFgOffsetY

      el.style.setProperty('--cam-rot-x', `${rotX.toFixed(2)}deg`)
      el.style.setProperty('--cam-rot-y', `${rotY.toFixed(2)}deg`)
      el.style.setProperty('--cam-bg-x', `${bgX.toFixed(2)}px`)
      el.style.setProperty('--cam-bg-y', `${bgY.toFixed(2)}px`)
      el.style.setProperty('--cam-fg-x', `${fgX.toFixed(2)}px`)
      el.style.setProperty('--cam-fg-y', `${fgY.toFixed(2)}px`)

      rafId = requestAnimationFrame(updateLoop)
    }

    rafId = requestAnimationFrame(updateLoop)

    // 鼠标移动事件监听
    const handleMouseMove = (e: MouseEvent) => {
      const rect = el.getBoundingClientRect()
      if (rect.width <= 0 || rect.height <= 0) return

      // 归一化中心坐标 (-1 ~ 1)
      const normX = ((e.clientX - rect.left) / rect.width - 0.5) * 2
      const normY = ((e.clientY - rect.top) / rect.height - 0.5) * 2

      targetX = Math.max(-1, Math.min(1, normX))
      targetY = Math.max(-1, Math.min(1, normY))

      // 计算鼠标移动速率（用于樱花风力扰动）
      const now = performance.now()
      const dt = Math.max(1, now - lastTime)
      const dx = e.clientX - lastMouseX
      const dy = e.clientY - lastMouseY
      const speed = Math.sqrt(dx * dx + dy * dy) / dt

      lastTime = now
      lastMouseX = e.clientX
      lastMouseY = e.clientY

      if (onMouseMoveCoords) {
        onMouseMoveCoords(normX, normY, speed)
      }
    }

    // 鼠标移出时缓缓回正视角
    const handleMouseLeave = () => {
      targetX = 0
      targetY = 0
    }

    // 移动端陀螺仪视差支持
    const handleDeviceOrientation = (e: DeviceOrientationEvent) => {
      if (e.gamma === null || e.beta === null) return
      // 限制倾斜范围
      const gamma = Math.max(-30, Math.min(30, e.gamma))
      const beta = Math.max(-30, Math.min(30, e.beta - 45)) // 针对握持手机视角

      targetX = gamma / 30
      targetY = beta / 30
    }

    window.addEventListener('mousemove', handleMouseMove, { passive: true })
    window.addEventListener('mouseleave', handleMouseLeave)

    if (window.DeviceOrientationEvent) {
      window.addEventListener('deviceorientation', handleDeviceOrientation, {
        passive: true,
      })
    }

    return () => {
      if (rafId !== undefined) {
        cancelAnimationFrame(rafId)
      }
      window.removeEventListener('mousemove', handleMouseMove)
      window.removeEventListener('mouseleave', handleMouseLeave)
      if (window.DeviceOrientationEvent) {
        window.removeEventListener('deviceorientation', handleDeviceOrientation)
      }
    }
  }, [intensity, onMouseMoveCoords])

  return (
    <div
      ref={containerRef}
      className={cn(
        'game-parallax-viewport relative h-screen w-screen overflow-hidden select-none',
        className
      )}
      style={
        {
          '--cam-rot-x': '0deg',
          '--cam-rot-y': '0deg',
          '--cam-bg-x': '0px',
          '--cam-bg-y': '0px',
          '--cam-fg-x': '0px',
          '--cam-fg-y': '0px',
          perspective: '1100px',
          perspectiveOrigin: '50% 50%',
        } as React.CSSProperties
      }
    >
      {children}
    </div>
  )
}
