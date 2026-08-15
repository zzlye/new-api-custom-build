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

import { useAppearance } from '@/hooks/use-appearance'
import { cn } from '@/lib/utils'

interface Petal {
  x: number
  y: number
  z: number
  size: number
  speedY: number
  speedX: number
  rotation: number
  rotationSpeed: number
  flip: number
  flipSpeed: number
  opacity: number
  swayAmplitude: number
  swaySpeed: number
  swayPhase: number
}

interface HomeSakuraProps {
  className?: string
}

/**
 * 主页落樱花瓣粒子特效
 * 支持在外观设置中自由开启/关闭，与第一人称视角产生三维景深视差漂移
 */
export function HomeSakura({ className }: HomeSakuraProps) {
  const appearance = useAppearance()
  const canvasRef = useRef<HTMLCanvasElement>(null)

  // 判断外观设置是否开启了主页花瓣
  const isEnabled = appearance.home_sakura !== false

  useEffect(() => {
    if (!isEnabled) return
    const canvas = canvasRef.current
    if (!canvas) return
    const ctx = canvas.getContext('2d')
    if (!ctx) return

    let animationFrameId: number
    let width = (canvas.width = window.innerWidth)
    let height = (canvas.height = window.innerHeight)

    const handleResize = () => {
      if (!canvas) return
      width = canvas.width = window.innerWidth
      height = canvas.height = window.innerHeight
    }
    window.addEventListener('resize', handleResize)

    // 创建落樱粒子池
    const particleCount = Math.min(38, Math.floor(width / 35))
    const petals: Petal[] = []

    for (let i = 0; i < particleCount; i++) {
      petals.push({
        x: Math.random() * width,
        y: Math.random() * height - height,
        z: Math.random() * 0.8 + 0.4,
        size: Math.random() * 7 + 8,
        speedY: Math.random() * 0.9 + 0.7,
        speedX: (Math.random() - 0.4) * 0.6,
        rotation: Math.random() * Math.PI * 2,
        rotationSpeed: (Math.random() - 0.5) * 0.02,
        flip: Math.random() * Math.PI,
        flipSpeed: Math.random() * 0.03 + 0.015,
        opacity: Math.random() * 0.5 + 0.4,
        swayAmplitude: Math.random() * 1.5 + 0.8,
        swaySpeed: Math.random() * 0.02 + 0.01,
        swayPhase: Math.random() * Math.PI * 2,
      })
    }

    let time = 0

    const render = () => {
      time += 1
      ctx.clearRect(0, 0, width, height)

      for (let i = 0; i < petals.length; i++) {
        const p = petals[i]

        // 飘落物理模拟
        p.y += p.speedY * p.z
        p.x += (p.speedX + Math.sin(time * p.swaySpeed + p.swayPhase) * p.swayAmplitude) * p.z
        p.rotation += p.rotationSpeed
        p.flip += p.flipSpeed

        // 边界循环再生
        if (p.y > height + 20) {
          p.y = -20
          p.x = Math.random() * width
        }
        if (p.x > width + 20) p.x = -20
        if (p.x < -20) p.x = width + 20

        // 绘制三维旋转樱花瓣
        ctx.save()
        ctx.translate(p.x, p.y)
        ctx.rotate(p.rotation)
        ctx.scale(p.z, p.z * Math.cos(p.flip))

        // 渐变樱花粉色
        const grad = ctx.createRadialGradient(0, 0, 1, 0, 0, p.size)
        grad.addColorStop(0, `rgba(255, 225, 235, ${p.opacity})`)
        grad.addColorStop(0.5, `rgba(251, 113, 133, ${p.opacity * 0.85})`)
        grad.addColorStop(1, `rgba(244, 63, 94, ${p.opacity * 0.6})`)

        ctx.fillStyle = grad
        ctx.beginPath()

        // 逼真花瓣贝塞尔曲线
        ctx.moveTo(0, -p.size)
        ctx.bezierCurveTo(
          p.size * 0.6,
          -p.size * 0.7,
          p.size * 0.8,
          p.size * 0.3,
          0,
          p.size
        )
        ctx.bezierCurveTo(
          -p.size * 0.8,
          p.size * 0.3,
          -p.size * 0.6,
          -p.size * 0.7,
          0,
          -p.size
        )
        ctx.closePath()
        ctx.fill()

        ctx.restore()
      }

      animationFrameId = requestAnimationFrame(render)
    }

    animationFrameId = requestAnimationFrame(render)

    return () => {
      window.removeEventListener('resize', handleResize)
      cancelAnimationFrame(animationFrameId)
    }
  }, [isEnabled])

  if (!isEnabled) return null

  return (
    <canvas
      ref={canvasRef}
      aria-hidden
      className={cn(
        'pointer-events-none absolute inset-0 z-[5] size-full overflow-hidden',
        className
      )}
    />
  )
}
