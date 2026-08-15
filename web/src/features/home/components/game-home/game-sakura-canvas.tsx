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

import { cn } from '@/lib/utils'

import type { SakuraDensity } from './game-types'

interface GameSakuraCanvasProps {
  density?: SakuraDensity
  className?: string
}

interface Petal {
  x: number
  y: number
  z: number // 景深深度 (0.2 ~ 1.5)
  size: number
  speedX: number
  speedY: number
  rotationX: number
  rotationY: number
  rotationZ: number
  rotSpeedX: number
  rotSpeedY: number
  rotSpeedZ: number
  oscillationAngle: number
  oscillationSpeed: number
  opacity: number
  colorType: number // 0: 粉红, 1: 绯红, 2: 浅白
}

interface Ember {
  x: number
  y: number
  size: number
  speedX: number
  speedY: number
  opacity: number
  maxOpacity: number
  life: number
  maxLife: number
}

/**
 * 3D 漫天落樱与神社灵火粒子系统
 * 真实模拟樱花翻转、风力飘荡与鼠标动力学扰动
 */
export function GameSakuraCanvas({
  density = 'medium',
  className,
}: GameSakuraCanvasProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null)

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas || density === 'off') return

    const ctx = canvas.getContext('2d')
    if (!ctx) return

    let rafId: number | undefined
    let width = (canvas.width = window.innerWidth)
    let height = (canvas.height = window.innerHeight)

    // 根据密度配置粒子数量
    const countMap: Record<SakuraDensity, number> = {
      off: 0,
      low: 28,
      medium: 52,
      high: 86,
    }
    const totalPetals = countMap[density]
    const totalEmbers = Math.floor(totalPetals * 0.4)

    // 初始化樱花粒子
    const petals: Petal[] = []
    for (let i = 0; i < totalPetals; i++) {
      petals.push(createPetal(width, height, true))
    }

    // 初始化灵火微粒
    const embers: Ember[] = []
    for (let i = 0; i < totalEmbers; i++) {
      embers.push(createEmber(width, height, true))
    }

    // 鼠标风力动力学
    let mouseX = -1000
    let mouseY = -1000
    let mouseVelX = 0
    let mouseVelY = 0
    let lastMouseX = -1000
    let lastMouseY = -1000
    let lastMouseTime = performance.now()

    function createPetal(w: number, h: number, randomY = false): Petal {
      const z = 0.3 + Math.random() * 1.1
      return {
        x: Math.random() * (w + 200) - 100,
        y: randomY ? Math.random() * h : -40 - Math.random() * 60,
        z,
        size: (9 + Math.random() * 8) * z,
        speedX: (0.6 + Math.random() * 1.4) * z,
        speedY: (1.1 + Math.random() * 1.6) * z,
        rotationX: Math.random() * Math.PI * 2,
        rotationY: Math.random() * Math.PI * 2,
        rotationZ: Math.random() * Math.PI * 2,
        rotSpeedX: 0.015 + Math.random() * 0.03,
        rotSpeedY: 0.012 + Math.random() * 0.025,
        rotSpeedZ: 0.01 + Math.random() * 0.02,
        oscillationAngle: Math.random() * Math.PI * 2,
        oscillationSpeed: 0.02 + Math.random() * 0.03,
        opacity: (0.45 + Math.random() * 0.45) * Math.min(1, z * 0.9),
        colorType: Math.random() < 0.6 ? 0 : Math.random() < 0.85 ? 1 : 2,
      }
    }

    function createEmber(w: number, h: number, randomY = false): Ember {
      const maxLife = 120 + Math.random() * 180
      return {
        x: Math.random() * w,
        y: randomY ? Math.random() * h : h + 10 + Math.random() * 30,
        size: 1.5 + Math.random() * 2.5,
        speedX: (Math.random() - 0.5) * 0.8,
        speedY: -(0.5 + Math.random() * 1.2),
        opacity: 0,
        maxOpacity: 0.3 + Math.random() * 0.5,
        life: randomY ? Math.random() * maxLife : 0,
        maxLife,
      }
    }

    // 绘制单片樱花花瓣
    function drawPetal(ctx: CanvasRenderingContext2D, p: Petal) {
      ctx.save()
      ctx.translate(p.x, p.y)

      // 3D 旋转缩放变换
      const scaleX = Math.cos(p.rotationX)
      const scaleY = Math.sin(p.rotationY)
      ctx.rotate(p.rotationZ)
      ctx.scale(scaleX, scaleY)

      ctx.globalAlpha = p.opacity

      // 花瓣渐变色
      const gradient = ctx.createLinearGradient(
        -p.size * 0.5,
        -p.size * 0.8,
        p.size * 0.5,
        p.size * 0.8
      )
      if (p.colorType === 0) {
        gradient.addColorStop(0, '#fde2e8')
        gradient.addColorStop(0.6, '#fb7185')
        gradient.addColorStop(1, '#e11d48')
      } else if (p.colorType === 1) {
        gradient.addColorStop(0, '#ffe4e6')
        gradient.addColorStop(0.5, '#f43f5e')
        gradient.addColorStop(1, '#9f1239')
      } else {
        gradient.addColorStop(0, '#ffffff')
        gradient.addColorStop(0.7, '#fbcfe8')
        gradient.addColorStop(1, '#fda4af')
      }

      ctx.fillStyle = gradient

      // 精确贝塞尔曲线绘制二次元花瓣形状
      ctx.beginPath()
      ctx.moveTo(0, -p.size)
      ctx.bezierCurveTo(
        p.size * 0.75,
        -p.size * 0.7,
        p.size * 0.8,
        p.size * 0.4,
        0,
        p.size
      )
      ctx.bezierCurveTo(
        -p.size * 0.8,
        p.size * 0.4,
        -p.size * 0.75,
        -p.size * 0.7,
        0,
        -p.size
      )
      ctx.closePath()
      ctx.fill()

      ctx.restore()
    }

    // 绘制灵火微粒
    function drawEmber(ctx: CanvasRenderingContext2D, e: Ember) {
      ctx.save()
      ctx.globalAlpha = e.opacity
      ctx.fillStyle = '#ff8855'
      ctx.shadowColor = '#f43f5e'
      ctx.shadowBlur = 8

      ctx.beginPath()
      ctx.arc(e.x, e.y, e.size, 0, Math.PI * 2)
      ctx.fill()
      ctx.restore()
    }

    // 主动画渲染循环
    const render = () => {
      ctx.clearRect(0, 0, width, height)

      // 更新并绘制灵火
      for (let i = 0; i < embers.length; i++) {
        const e = embers[i]
        e.life += 1
        e.x += e.speedX
        e.y += e.speedY

        // 淡入淡出包络
        if (e.life < 30) {
          e.opacity = (e.life / 30) * e.maxOpacity
        } else if (e.life > e.maxLife - 30) {
          e.opacity = ((e.maxLife - e.life) / 30) * e.maxOpacity
        } else {
          e.opacity = e.maxOpacity
        }

        if (e.life >= e.maxLife || e.y < -20) {
          embers[i] = createEmber(width, height, false)
        } else {
          drawEmber(ctx, e)
        }
      }

      // 更新并绘制樱花
      for (let i = 0; i < petals.length; i++) {
        const p = petals[i]

        p.oscillationAngle += p.oscillationSpeed
        const windSway = Math.sin(p.oscillationAngle) * 0.85

        p.rotationX += p.rotSpeedX
        p.rotationY += p.rotSpeedY
        p.rotationZ += p.rotSpeedZ

        p.x += p.speedX + windSway
        p.y += p.speedY

        // 鼠标风力动力学相互作用
        const dx = p.x - mouseX
        const dy = p.y - mouseY
        const distSq = dx * dx + dy * dy
        const maxDist = 180
        if (distSq < maxDist * maxDist && distSq > 1) {
          const dist = Math.sqrt(distSq)
          const force = (1 - dist / maxDist) * 3.5
          p.x += (dx / dist) * force + mouseVelX * 0.15
          p.y += (dy / dist) * force + mouseVelY * 0.15
        }

        // 超出边界后重新从顶部或左侧循环生成
        if (p.y > height + 50 || p.x > width + 100 || p.x < -100) {
          petals[i] = createPetal(width, height, false)
        } else {
          drawPetal(ctx, p)
        }
      }

      // 衰减鼠标速度
      mouseVelX *= 0.9
      mouseVelY *= 0.9

      rafId = requestAnimationFrame(render)
    }

    rafId = requestAnimationFrame(render)

    // 窗口尺寸自适应
    const handleResize = () => {
      width = canvas.width = window.innerWidth
      height = canvas.height = window.innerHeight
    }

    // 鼠标风力计算
    const handleMouseMove = (e: MouseEvent) => {
      const now = performance.now()
      const dt = Math.max(1, now - lastMouseTime)
      if (lastMouseX !== -1000) {
        mouseVelX = ((e.clientX - lastMouseX) / dt) * 12
        mouseVelY = ((e.clientY - lastMouseY) / dt) * 12
      }
      mouseX = e.clientX
      mouseY = e.clientY
      lastMouseX = e.clientX
      lastMouseY = e.clientY
      lastMouseTime = now
    }

    window.addEventListener('resize', handleResize)
    window.addEventListener('mousemove', handleMouseMove, { passive: true })

    return () => {
      if (rafId !== undefined) cancelAnimationFrame(rafId)
      window.removeEventListener('resize', handleResize)
      window.removeEventListener('mousemove', handleMouseMove)
    }
  }, [density])

  if (density === 'off') return null

  return (
    <canvas
      ref={canvasRef}
      className={cn(
        'game-sakura-canvas pointer-events-none absolute inset-0 -z-10 size-full',
        className
      )}
      style={{
        transform:
          'translate3d(var(--cam-fg-x), var(--cam-fg-y), 20px) rotateX(calc(var(--cam-rot-x) * 0.3)) rotateY(calc(var(--cam-rot-y) * 0.3))',
        transformOrigin: '50% 50%',
      }}
    />
  )
}
