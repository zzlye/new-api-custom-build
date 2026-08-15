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
import * as THREE from 'three'

import { cn } from '@/lib/utils'

import {
  createPanoramaPatchData,
  projectPointerToPanorama,
} from './webgl-panorama-math'

const CAMERA_FOV = 82
const ACTIVE_FRAME_INTERVAL = 1000 / 30
const IDLE_FRAME_INTERVAL = 1000 / 15

/** 统一判断当前页面是否仍接收用户交互。 */
function isPageFocused(): boolean {
  return typeof document.hasFocus !== 'function' || document.hasFocus()
}

type WebGLPanoramaCanvasProps = {
  className?: string
  media: string
  mediaType: 'image' | 'video'
  onUnavailable: () => void
  overlayOpacity: number
  suspendVideo?: boolean
}

/** 将主页图片或视频作为纹理渲染到第一人称内凹半球。 */
export function WebGLPanoramaCanvas({
  className,
  media,
  mediaType,
  onUnavailable,
  overlayOpacity,
  suspendVideo = false,
}: WebGLPanoramaCanvasProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const videoRef = useRef<HTMLVideoElement | null>(null)
  const suspendVideoRef = useRef(suspendVideo)

  useEffect(() => {
    suspendVideoRef.current = suspendVideo
    const video = videoRef.current
    if (!video) return

    if (document.hidden || !isPageFocused()) {
      video.pause()
      if (video.hasAttribute('src')) {
        video.removeAttribute('src')
        video.load()
      }
      return
    }

    if (suspendVideo) {
      video.pause()
      return
    }
    if (video.getAttribute('src') !== media) {
      video.setAttribute('src', media)
      video.load()
    }
    void video.play().catch(() => {})
  }, [media, suspendVideo])

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas || !media) return

    let renderer: THREE.WebGLRenderer
    try {
      renderer = new THREE.WebGLRenderer({
        alpha: true,
        antialias: false,
        canvas,
        powerPreference: 'low-power',
      })
    } catch {
      onUnavailable()
      return
    }

    renderer.outputColorSpace = THREE.SRGBColorSpace
    renderer.setClearColor(0x000000, 0)
    renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 1.25))

    const scene = new THREE.Scene()
    const camera = new THREE.PerspectiveCamera(CAMERA_FOV, 1, 0.1, 100)
    camera.rotation.order = 'YXZ'

    const patch = createPanoramaPatchData()
    const geometry = new THREE.BufferGeometry()
    geometry.setAttribute(
      'position',
      new THREE.BufferAttribute(patch.positions, 3)
    )
    geometry.setAttribute('uv', new THREE.BufferAttribute(patch.uvs, 2))
    geometry.setIndex(new THREE.BufferAttribute(patch.indices, 1))

    const shade = 1 - THREE.MathUtils.clamp(overlayOpacity, 0, 1)
    const material = new THREE.MeshBasicMaterial({
      color: new THREE.Color().setRGB(
        shade,
        shade,
        shade,
        THREE.SRGBColorSpace
      ),
      side: THREE.DoubleSide,
      toneMapped: false,
    })
    const mesh = new THREE.Mesh(geometry, material)
    mesh.frustumCulled = false
    mesh.visible = false
    scene.add(mesh)

    let disposed = false
    let unavailableReported = false
    let texture: THREE.Texture | null = null
    let animationFrameId = 0
    let lastRenderedAt = 0
    let needsRender = true
    let currentYaw = 0
    let currentPitch = 0
    let targetYaw = 0
    let targetPitch = 0
    let pageFocused = isPageFocused()

    const reportUnavailable = () => {
      if (disposed || unavailableReported) return
      unavailableReported = true
      window.cancelAnimationFrame(animationFrameId)
      onUnavailable()
    }

    const markReady = (nextTexture: THREE.Texture) => {
      if (disposed || unavailableReported) {
        nextTexture.dispose()
        return
      }
      texture = nextTexture
      texture.colorSpace = THREE.SRGBColorSpace
      texture.minFilter = THREE.LinearFilter
      texture.magFilter = THREE.LinearFilter
      texture.generateMipmaps = false
      material.map = texture
      material.needsUpdate = true
      mesh.visible = true
      needsRender = true
      canvas.dataset.panoramaReady = 'true'
    }

    const handleMediaError = () => {
      const video = videoRef.current
      if (mediaType === 'video' && video && !video.hasAttribute('src')) return
      reportUnavailable()
    }

    if (mediaType === 'video') {
      const video = document.createElement('video')
      video.crossOrigin = 'anonymous'
      video.loop = true
      video.muted = true
      video.playsInline = true
      video.preload = 'none'
      video.disablePictureInPicture = true
      video.disableRemotePlayback = true
      videoRef.current = video
      video.addEventListener(
        'loadeddata',
        () => {
          markReady(new THREE.VideoTexture(video))
        },
        { once: true }
      )
      video.addEventListener('error', handleMediaError)
    } else {
      const loader = new THREE.TextureLoader()
      loader.setCrossOrigin('anonymous')
      loader.load(media, markReady, undefined, handleMediaError)
    }

    const handleResize = () => {
      const width = Math.max(1, canvas.clientWidth)
      const height = Math.max(1, canvas.clientHeight)
      camera.aspect = width / height
      camera.updateProjectionMatrix()
      renderer.setSize(width, height, false)
      needsRender = true
    }

    const handlePointerMove = (event: PointerEvent) => {
      const bounds = canvas.getBoundingClientRect()
      if (bounds.width <= 0 || bounds.height <= 0) return
      const normalizedX =
        (event.clientX - bounds.left - bounds.width / 2) / (bounds.width / 2)
      const normalizedY =
        (event.clientY - bounds.top - bounds.height / 2) / (bounds.height / 2)
      const projected = projectPointerToPanorama(normalizedX, normalizedY)
      targetYaw = projected.yaw
      targetPitch = projected.pitch
    }

    const syncPlayback = () => {
      const video = videoRef.current
      if (!video) return
      pageFocused = isPageFocused()
      if (!pageFocused || document.hidden) {
        video.pause()
        if (video.hasAttribute('src')) {
          video.removeAttribute('src')
          video.load()
        }
        return
      }
      if (suspendVideoRef.current) {
        video.pause()
        return
      }
      if (video.getAttribute('src') !== media) {
        video.setAttribute('src', media)
        video.load()
      }
      void video.play().catch(() => {})
    }

    const handleVisibilityChange = () => {
      syncPlayback()
    }
    const handleWindowBlur = () => {
      syncPlayback()
    }
    const handleWindowFocus = () => {
      syncPlayback()
    }

    const render = (now: number) => {
      if (unavailableReported) return
      animationFrameId = window.requestAnimationFrame(render)
      if (!pageFocused || document.hidden) return

      const yawDelta = targetYaw - currentYaw
      const pitchDelta = targetPitch - currentPitch
      const moving =
        Math.abs(yawDelta) > 0.0005 || Math.abs(pitchDelta) > 0.0005
      // 静态图片和暂停视频只按需绘制，播放中的视频维持低帧率更新。
      const video = videoRef.current
      const videoAdvancing = Boolean(
        video &&
        !video.paused &&
        video.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA
      )
      if (!moving && !videoAdvancing && !needsRender) return

      let frameInterval = 0
      if (moving) {
        frameInterval = ACTIVE_FRAME_INTERVAL
      } else if (videoAdvancing) {
        frameInterval = IDLE_FRAME_INTERVAL
      }
      if (now - lastRenderedAt < frameInterval) return

      currentYaw += yawDelta * 0.14
      currentPitch += pitchDelta * 0.14
      camera.rotation.y = -currentYaw
      camera.rotation.x = -currentPitch
      try {
        renderer.render(scene, camera)
      } catch {
        reportUnavailable()
        return
      }
      needsRender = false
      lastRenderedAt = now
    }

    const handleContextLost = (event: Event) => {
      event.preventDefault()
      reportUnavailable()
    }

    handleResize()
    canvas.addEventListener('webglcontextlost', handleContextLost)
    window.addEventListener('resize', handleResize)
    window.addEventListener('pointermove', handlePointerMove, {
      passive: true,
    })
    window.addEventListener('blur', handleWindowBlur)
    window.addEventListener('focus', handleWindowFocus)
    document.addEventListener('visibilitychange', handleVisibilityChange)
    syncPlayback()
    animationFrameId = window.requestAnimationFrame(render)

    return () => {
      disposed = true
      window.cancelAnimationFrame(animationFrameId)
      canvas.removeEventListener('webglcontextlost', handleContextLost)
      window.removeEventListener('resize', handleResize)
      window.removeEventListener('pointermove', handlePointerMove)
      window.removeEventListener('blur', handleWindowBlur)
      window.removeEventListener('focus', handleWindowFocus)
      document.removeEventListener('visibilitychange', handleVisibilityChange)

      const video = videoRef.current
      if (video) {
        video.pause()
        video.removeAttribute('src')
        video.load()
        videoRef.current = null
      }
      texture?.dispose()
      material.dispose()
      geometry.dispose()
      renderer.dispose()
      renderer.forceContextLoss()
      delete canvas.dataset.panoramaReady
    }
  }, [media, mediaType, onUnavailable, overlayOpacity])

  return (
    <canvas
      ref={canvasRef}
      aria-hidden
      data-home-panorama='true'
      className={cn(
        'pointer-events-none absolute inset-0 z-0 size-full select-none overflow-hidden',
        className
      )}
    />
  )
}
