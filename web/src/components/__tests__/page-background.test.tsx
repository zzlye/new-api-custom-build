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
import assert from 'node:assert/strict'
import { after, beforeEach, describe, test } from 'node:test'

import { Window } from 'happy-dom'

const domWindow = new Window({ url: 'http://localhost/' })
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLMediaElement',
  'HTMLVideoElement',
  'Node',
  'Element',
  'Event',
  'CustomEvent',
  'MutationObserver',
] as const

for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}
Object.defineProperty(domWindow.document, 'hidden', {
  configurable: true,
  value: false,
})

const playbackCalls = { load: 0, pause: 0, play: 0 }
let timerId = 0
const pendingTimers = new Map<number, () => void>()
const originalSetTimeout = domWindow.setTimeout.bind(domWindow)
const originalClearTimeout = domWindow.clearTimeout.bind(domWindow)
Object.defineProperty(domWindow, 'setTimeout', {
  configurable: true,
  value: (callback: () => void) => {
    timerId += 1
    pendingTimers.set(timerId, callback)
    return timerId
  },
})
Object.defineProperty(domWindow, 'clearTimeout', {
  configurable: true,
  value: (id: number) => {
    pendingTimers.delete(id)
  },
})
Object.defineProperty(domWindow.HTMLMediaElement.prototype, 'pause', {
  configurable: true,
  value: () => {
    playbackCalls.pause += 1
  },
})
Object.defineProperty(domWindow.HTMLMediaElement.prototype, 'play', {
  configurable: true,
  value: () => {
    playbackCalls.play += 1
    return Promise.resolve()
  },
})
Object.defineProperty(domWindow.HTMLMediaElement.prototype, 'load', {
  configurable: true,
  value: () => {
    playbackCalls.load += 1
  },
})

const { act, StrictMode } = await import('react')
const { createRoot } = await import('react-dom/client')
const { PageBackground } = await import('../page-background')

;(
  globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
).IS_REACT_ACT_ENVIRONMENT = true

describe('视频页面背景', () => {
  beforeEach(() => {
    playbackCalls.load = 0
    playbackCalls.pause = 0
    playbackCalls.play = 0
    pendingTimers.clear()
    delete document.documentElement.dataset.videoBackground
    delete document.documentElement.dataset.videoBackgroundCount
  })

  after(() => {
    Object.defineProperty(domWindow, 'setTimeout', {
      configurable: true,
      value: originalSetTimeout,
    })
    Object.defineProperty(domWindow, 'clearTimeout', {
      configurable: true,
      value: originalClearTimeout,
    })
    domWindow.close()
  })

  test('切页时暂停并在页面稳定后恢复播放', async () => {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    const config = {
      type: 'video' as const,
      color: '#000000',
      media: '/background.mp4',
    }

    await act(async () => {
      root.render(
        <PageBackground
          config={config}
          suspendVideo
          videoPlaybackKey='/profile'
        />
      )
    })

    const video = container.querySelector('video')
    assert.ok(video)
    assert.equal(video.getAttribute('preload'), 'metadata')
    assert.ok(playbackCalls.pause > 0)
    assert.equal(playbackCalls.play, 0)

    await act(async () => {
      root.render(<PageBackground config={config} videoPlaybackKey='/wallet' />)
    })
    await act(async () => {
      for (const callback of pendingTimers.values()) callback()
      pendingTimers.clear()
    })

    assert.ok(playbackCalls.play > 0)

    const pauseCountBeforeUnmount = playbackCalls.pause
    await act(async () => root.unmount())

    assert.ok(playbackCalls.pause > pauseCountBeforeUnmount)
    assert.equal(playbackCalls.load, 1)
    assert.equal(video.getAttribute('src'), null)
    assert.equal(document.documentElement.dataset.videoBackground, undefined)
    assert.equal(
      document.documentElement.dataset.videoBackgroundCount,
      undefined
    )
    container.remove()
  })

  test('多个视频背景同时存在时按实例维护动态背景标记', async () => {
    const firstContainer = document.createElement('div')
    const secondContainer = document.createElement('div')
    document.body.append(firstContainer, secondContainer)
    const firstRoot = createRoot(firstContainer)
    const secondRoot = createRoot(secondContainer)
    const config = {
      type: 'video' as const,
      color: '#000000',
      media: '/background.mp4',
    }

    await act(async () => {
      firstRoot.render(<PageBackground config={config} suspendVideo />)
      secondRoot.render(<PageBackground config={config} suspendVideo />)
    })

    assert.equal(document.documentElement.dataset.videoBackground, 'true')
    assert.equal(document.documentElement.dataset.videoBackgroundCount, '2')

    await act(async () => firstRoot.unmount())
    assert.equal(document.documentElement.dataset.videoBackground, 'true')
    assert.equal(document.documentElement.dataset.videoBackgroundCount, '1')

    await act(async () => secondRoot.unmount())
    assert.equal(document.documentElement.dataset.videoBackground, undefined)
    assert.equal(
      document.documentElement.dataset.videoBackgroundCount,
      undefined
    )
    firstContainer.remove()
    secondContainer.remove()
  })

  test('StrictMode 重复执行 effect 后仍保留一个动态背景实例', async () => {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    const config = {
      type: 'video' as const,
      color: '#000000',
      media: '/background.mp4',
    }

    await act(async () => {
      root.render(
        <StrictMode>
          <PageBackground config={config} suspendVideo />
        </StrictMode>
      )
    })

    assert.equal(document.documentElement.dataset.videoBackground, 'true')
    assert.equal(document.documentElement.dataset.videoBackgroundCount, '1')

    await act(async () => root.unmount())
    assert.equal(document.documentElement.dataset.videoBackground, undefined)
    assert.equal(
      document.documentElement.dataset.videoBackgroundCount,
      undefined
    )
    container.remove()
  })
})
