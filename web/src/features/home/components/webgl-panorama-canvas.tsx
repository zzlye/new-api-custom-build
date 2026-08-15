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
import {
  getEffectiveHomeBackground,
  getEffectiveHomeBackgroundOverlayOpacity,
} from '@/lib/appearance'
import { cn } from '@/lib/utils'

// 4x4 矩阵运算辅助函数
function createMat4(): Float32Array {
  const out = new Float32Array(16)
  out[0] = 1
  out[5] = 1
  out[10] = 1
  out[15] = 1
  return out
}

function mat4Perspective(
  out: Float32Array,
  fovy: number,
  aspect: number,
  near: number,
  far: number
) {
  const f = 1.0 / Math.tan(fovy / 2)
  const nf = 1 / (near - far)
  out[0] = f / aspect
  out[1] = 0
  out[2] = 0
  out[3] = 0
  out[4] = 0
  out[5] = f
  out[6] = 0
  out[7] = 0
  out[8] = 0
  out[9] = 0
  out[10] = (far + near) * nf
  out[11] = -1
  out[12] = 0
  out[13] = 0
  out[14] = 2 * far * near * nf
  out[15] = 0
}

function mat4RotateX(out: Float32Array, a: Float32Array, rad: number) {
  const s = Math.sin(rad)
  const c = Math.cos(rad)
  const a10 = a[4]
  const a11 = a[5]
  const a12 = a[6]
  const a13 = a[7]
  const a20 = a[8]
  const a21 = a[9]
  const a22 = a[10]
  const a23 = a[11]

  if (a !== out) {
    out[0] = a[0]
    out[1] = a[1]
    out[2] = a[2]
    out[3] = a[3]
    out[12] = a[12]
    out[13] = a[13]
    out[14] = a[14]
    out[15] = a[15]
  }

  out[4] = a10 * c + a20 * s
  out[5] = a11 * c + a21 * s
  out[6] = a12 * c + a22 * s
  out[7] = a13 * c + a23 * s
  out[8] = a20 * c - a10 * s
  out[9] = a21 * c - a11 * s
  out[10] = a22 * c - a12 * s
  out[11] = a23 * c - a13 * s
}

function mat4RotateY(out: Float32Array, a: Float32Array, rad: number) {
  const s = Math.sin(rad)
  const c = Math.cos(rad)
  const a00 = a[0]
  const a01 = a[1]
  const a02 = a[2]
  const a03 = a[3]
  const a20 = a[8]
  const a21 = a[9]
  const a22 = a[10]
  const a23 = a[11]

  if (a !== out) {
    out[4] = a[4]
    out[5] = a[5]
    out[6] = a[6]
    out[7] = a[7]
    out[12] = a[12]
    out[13] = a[13]
    out[14] = a[14]
    out[15] = a[15]
  }

  out[0] = a00 * c - a20 * s
  out[1] = a01 * c - a21 * s
  out[2] = a02 * c - a22 * s
  out[3] = a03 * c - a23 * s
  out[8] = a00 * s + a20 * c
  out[9] = a01 * s + a21 * c
  out[10] = a02 * s + a22 * c
  out[11] = a03 * s + a23 * c
}

const VS_SOURCE = `
attribute vec3 aPosition;
attribute vec2 aTexCoord;
uniform mat4 uProjection;
uniform mat4 uView;
varying vec2 vTexCoord;

void main() {
  vTexCoord = aTexCoord;
  gl_Position = uProjection * uView * vec4(aPosition, 1.0);
}
`

const FS_SOURCE = `
precision highp float;
varying vec2 vTexCoord;
uniform sampler2D uSampler;
uniform float uHasTexture;
uniform float uOverlayOpacity;
uniform float uTime;

void main() {
  vec4 texColor;
  if (uHasTexture > 0.5) {
    texColor = texture2D(uSampler, vTexCoord);
  } else {
    // 默认高质感深邃天球与环形图腾渐变
    float y = vTexCoord.y;
    float x = vTexCoord.x;
    vec3 topColor = vec3(0.04, 0.07, 0.16);
    vec3 midColor = vec3(0.08, 0.05, 0.15);
    vec3 botColor = vec3(0.02, 0.03, 0.07);
    vec3 base = mix(botColor, midColor, smoothstep(0.0, 0.5, y));
    base = mix(base, topColor, smoothstep(0.5, 1.0, y));

    // 图腾环线与全景光晕
    float totemRing = sin(y * 32.0 + sin(x * 16.0 + uTime * 0.3)) * 0.025 + 0.025;
    float subtleGrid = (sin(x * 80.0) * sin(y * 40.0)) * 0.015;
    base += vec3(totemRing * 0.5, totemRing * 0.7, totemRing * 1.0) + vec3(subtleGrid);

    texColor = vec4(base, 1.0);
  }

  // 混合黑色遮罩
  texColor.rgb = mix(texColor.rgb, vec3(0.0), clamp(uOverlayOpacity, 0.0, 1.0));
  gl_FragColor = texColor;
}
`

interface WebGLPanoramaCanvasProps {
  className?: string
}

/**
 * 3D 全景球体/图腾柱曲面 WebGL 渲染视口
 * 将背景图片/视频投影至 3D 环形半球/图腾柱内壁，产生身临其境的第一人称转动曲面效果
 */
export function WebGLPanoramaCanvas({ className }: WebGLPanoramaCanvasProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const videoRef = useRef<HTMLVideoElement | null>(null)
  const appearance = useAppearance()
  const bgConfig = getEffectiveHomeBackground(appearance)
  const overlayOpacity = getEffectiveHomeBackgroundOverlayOpacity(appearance)

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return

    const gl =
      canvas.getContext('webgl', { alpha: false, antialias: true }) ||
      (canvas.getContext(
        'experimental-webgl'
      ) as WebGLRenderingContext | null)

    if (!gl) return

    // 编译着色器
    const compileShader = (type: number, source: string) => {
      const shader = gl.createShader(type)
      if (!shader) return null
      gl.shaderSource(shader, source)
      gl.compileShader(shader)
      if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
        gl.deleteShader(shader)
        return null
      }
      return shader
    }

    const vs = compileShader(gl.VERTEX_SHADER, VS_SOURCE)
    const fs = compileShader(gl.FRAGMENT_SHADER, FS_SOURCE)
    if (!vs || !fs) return

    const program = gl.createProgram()
    if (!program) return
    gl.attachShader(program, vs)
    gl.attachShader(program, fs)
    gl.linkProgram(program)

    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
      return
    }

    gl.useProgram(program)

    // 获取 Uniform 与 Attribute 索引
    const aPositionLoc = gl.getAttribLocation(program, 'aPosition')
    const aTexCoordLoc = gl.getAttribLocation(program, 'aTexCoord')
    const uProjLoc = gl.getUniformLocation(program, 'uProjection')
    const uViewLoc = gl.getUniformLocation(program, 'uView')
    const uSamplerLoc = gl.getUniformLocation(program, 'uSampler')
    const uHasTexLoc = gl.getUniformLocation(program, 'uHasTexture')
    const uOpacityLoc = gl.getUniformLocation(program, 'uOverlayOpacity')
    const uTimeLoc = gl.getUniformLocation(program, 'uTime')

    // 生成 3D 球体/曲面图腾网格 (Sphere / Panoramic Dome Mesh)
    const rings = 40
    const segments = 60
    const radius = 10.0
    const vertices: number[] = []
    const texCoords: number[] = []
    const indices: number[] = []

    for (let r = 0; r <= rings; r++) {
      const phi = (r / rings) * Math.PI // 0 到 PI
      for (let s = 0; s <= segments; s++) {
        const theta = (s / segments) * 2 * Math.PI // 0 到 2PI

        // 顶点坐标（球体内壁）
        const x = radius * Math.sin(phi) * Math.cos(theta)
        const y = radius * Math.cos(phi)
        const z = radius * Math.sin(phi) * Math.sin(theta)

        vertices.push(x, y, z)
        // 纹理 UV（水平镜像翻转，使从球体内部朝外看时文字与画面正向）
        texCoords.push(1.0 - s / segments, r / rings)
      }
    }

    for (let r = 0; r < rings; r++) {
      for (let s = 0; s < segments; s++) {
        const first = r * (segments + 1) + s
        const second = first + segments + 1

        // 面朝内部的三角形顶点索引
        indices.push(first, first + 1, second)
        indices.push(second, first + 1, second + 1)
      }
    }

    const vbo = gl.createBuffer()
    gl.bindBuffer(gl.ARRAY_BUFFER, vbo)
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array(vertices), gl.STATIC_DRAW)

    const tbo = gl.createBuffer()
    gl.bindBuffer(gl.ARRAY_BUFFER, tbo)
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array(texCoords), gl.STATIC_DRAW)

    const ibo = gl.createBuffer()
    gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, ibo)
    gl.bufferData(
      gl.ELEMENT_ARRAY_BUFFER,
      new Uint16Array(indices),
      gl.STATIC_DRAW
    )

    // 创建纹理
    const texture = gl.createTexture()
    gl.bindTexture(gl.TEXTURE_2D, texture)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)

    // 默认纯色占位像素
    gl.texImage2D(
      gl.TEXTURE_2D,
      0,
      gl.RGBA,
      1,
      1,
      0,
      gl.RGBA,
      gl.UNSIGNED_BYTE,
      new Uint8Array([15, 23, 42, 255])
    )

    let hasLoadedTexture = false

    // 视频或图片媒体资源加载
    let videoElem: HTMLVideoElement | null = null
    let imgElem: HTMLImageElement | null = null

    if (bgConfig.type === 'video' && bgConfig.media) {
      videoElem = document.createElement('video')
      videoElem.src = bgConfig.media
      videoElem.crossOrigin = 'anonymous'
      videoElem.loop = true
      videoElem.muted = true
      videoElem.playsInline = true
      videoElem.autoplay = true
      videoElem.play().catch(() => {})
      videoRef.current = videoElem
      hasLoadedTexture = true
    } else if (bgConfig.type === 'image' && bgConfig.media) {
      imgElem = new Image()
      imgElem.crossOrigin = 'anonymous'
      imgElem.src = bgConfig.media
      imgElem.onload = () => {
        if (!gl || !texture) return
        gl.bindTexture(gl.TEXTURE_2D, texture)
        gl.texImage2D(
          gl.TEXTURE_2D,
          0,
          gl.RGBA,
          gl.RGBA,
          gl.UNSIGNED_BYTE,
          imgElem!
        )
        hasLoadedTexture = true
      }
    }

    // 视角交互与物理阻尼
    let currentYaw = 0
    let currentPitch = 0
    let targetYaw = 0
    let targetPitch = 0
    let isDragging = false
    let startX = 0
    let startY = 0

    const handlePointerMove = (e: PointerEvent) => {
      // 鼠标移动根据屏幕中心偏移计算第一人称视角
      const rect = canvas.getBoundingClientRect()
      const normX = (e.clientX - rect.left - rect.width / 2) / (rect.width / 2)
      const normY = (e.clientY - rect.top - rect.height / 2) / (rect.height / 2)

      // 广角半球图腾环视范围（横向可达 ±45°，纵向可达 ±25°）
      targetYaw = normX * 0.65
      targetPitch = normY * 0.35
    }

    const handlePointerDown = (e: PointerEvent) => {
      isDragging = true
      startX = e.clientX
      startY = e.clientY
    }

    const handlePointerUp = () => {
      isDragging = false
    }

    const handleDragMove = (e: PointerEvent) => {
      if (!isDragging) return
      const dx = e.clientX - startX
      const dy = e.clientY - startY
      startX = e.clientX
      startY = e.clientY
      targetYaw += dx * 0.003
      targetPitch += dy * 0.003
    }

    window.addEventListener('pointermove', handlePointerMove, { passive: true })
    canvas.addEventListener('pointerdown', handlePointerDown)
    window.addEventListener('pointerup', handlePointerUp)
    window.addEventListener('pointermove', handleDragMove, { passive: true })

    const handleResize = () => {
      if (!canvas) return
      const dpr = Math.min(window.devicePixelRatio || 1, 2)
      canvas.width = Math.floor(canvas.clientWidth * dpr)
      canvas.height = Math.floor(canvas.clientHeight * dpr)
      gl.viewport(0, 0, canvas.width, canvas.height)
    }
    handleResize()
    window.addEventListener('resize', handleResize)

    const projMatrix = createMat4()
    const viewMatrix = createMat4()
    let animationFrameId: number
    let startTime = performance.now()

    const render = () => {
      const now = performance.now()
      const time = (now - startTime) / 1000

      // 平滑摄像机阻尼插值 (Camera Lerp)
      currentYaw += (targetYaw - currentYaw) * 0.08
      currentPitch += (targetPitch - currentPitch) * 0.08

      // 如果有正在播放的视频纹理，实时更新帧
      if (
        videoElem &&
        videoElem.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA
      ) {
        gl.bindTexture(gl.TEXTURE_2D, texture)
        gl.texImage2D(
          gl.TEXTURE_2D,
          0,
          gl.RGBA,
          gl.RGBA,
          gl.UNSIGNED_BYTE,
          videoElem
        )
      }

      gl.viewport(0, 0, canvas.width, canvas.height)
      gl.clearColor(0.05, 0.07, 0.12, 1.0)
      gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)
      gl.enable(gl.DEPTH_TEST)

      gl.useProgram(program)

      // 广角透视投影 (FOV 72°)
      const aspect = canvas.width / Math.max(1, canvas.height)
      mat4Perspective(projMatrix, (72 * Math.PI) / 180, aspect, 0.1, 100.0)

      // 摄像机视点旋转（第一人称 Pitch & Yaw 环视）
      const identity = createMat4()
      mat4RotateX(viewMatrix, identity, currentPitch)
      mat4RotateY(viewMatrix, viewMatrix, currentYaw)

      gl.uniformMatrix4fv(uProjLoc, false, projMatrix)
      gl.uniformMatrix4fv(uViewLoc, false, viewMatrix)
      gl.uniform1f(uOpacityLoc, overlayOpacity)
      gl.uniform1f(uTimeLoc, time)
      gl.uniform1f(uHasTexLoc, hasLoadedTexture ? 1.0 : 0.0)
      gl.uniform1i(uSamplerLoc, 0)

      gl.bindBuffer(gl.ARRAY_BUFFER, vbo)
      gl.enableVertexAttribArray(aPositionLoc)
      gl.vertexAttribPointer(aPositionLoc, 3, gl.FLOAT, false, 0, 0)

      gl.bindBuffer(gl.ARRAY_BUFFER, tbo)
      gl.enableVertexAttribArray(aTexCoordLoc)
      gl.vertexAttribPointer(aTexCoordLoc, 2, gl.FLOAT, false, 0, 0)

      gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, ibo)
      gl.drawElements(gl.TRIANGLES, indices.length, gl.UNSIGNED_SHORT, 0)

      animationFrameId = requestAnimationFrame(render)
    }

    animationFrameId = requestAnimationFrame(render)

    return () => {
      window.removeEventListener('pointermove', handlePointerMove)
      canvas.removeEventListener('pointerdown', handlePointerDown)
      window.removeEventListener('pointerup', handlePointerUp)
      window.removeEventListener('pointermove', handleDragMove)
      window.removeEventListener('resize', handleResize)
      cancelAnimationFrame(animationFrameId)

      if (videoElem) {
        videoElem.pause()
        videoElem.removeAttribute('src')
        videoElem.load()
      }

      gl.deleteBuffer(vbo)
      gl.deleteBuffer(tbo)
      gl.deleteBuffer(ibo)
      gl.deleteTexture(texture)
      gl.deleteProgram(program)
      gl.deleteShader(vs)
      gl.deleteShader(fs)
    }
  }, [bgConfig.type, bgConfig.media, overlayOpacity])

  return (
    <canvas
      ref={canvasRef}
      className={cn(
        'pointer-events-none absolute inset-0 -z-10 size-full select-none overflow-hidden',
        className
      )}
    />
  )
}
