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

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { ThemeCustomizationProvider, useThemeCustomization } =
  await import('../theme-customization-provider')
const { THEME_COOKIE_KEYS } = await import('@/lib/theme-customization')

;(
  globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
).IS_REACT_ACT_ENVIRONMENT = true

function BackgroundPreferenceProbe() {
  const { customization, resetCustomization, setBackgroundVisible } =
    useThemeCustomization()

  return (
    <div>
      <output data-testid='background-visible'>
        {String(customization.backgroundVisible)}
      </output>
      <button
        type='button'
        data-testid='toggle-background'
        onClick={() => setBackgroundVisible(!customization.backgroundVisible)}
      />
      <button
        type='button'
        data-testid='reset-background'
        onClick={resetCustomization}
      />
    </div>
  )
}

function clearBackgroundPreference() {
  document.cookie = `${THEME_COOKIE_KEYS.backgroundVisible}=; path=/; max-age=0`
}

describe('个人背景显示偏好', () => {
  beforeEach(() => {
    clearBackgroundPreference()
    document.body.replaceChildren()
  })

  after(() => domWindow.close())

  test('关闭背景后保存偏好并在重新挂载时恢复', async () => {
    const container = document.createElement('div')
    document.body.append(container)
    let root = createRoot(container)

    await act(async () => {
      root.render(
        <ThemeCustomizationProvider>
          <BackgroundPreferenceProbe />
        </ThemeCustomizationProvider>
      )
    })
    assert.equal(
      container.querySelector('[data-testid="background-visible"]')
        ?.textContent,
      'true'
    )

    await act(async () => {
      container
        .querySelector<HTMLButtonElement>('[data-testid="toggle-background"]')
        ?.click()
    })
    assert.equal(
      container.querySelector('[data-testid="background-visible"]')
        ?.textContent,
      'false'
    )
    assert.match(document.cookie, /theme_background_visible=false/)

    await act(async () => root.unmount())
    root = createRoot(container)
    await act(async () => {
      root.render(
        <ThemeCustomizationProvider>
          <BackgroundPreferenceProbe />
        </ThemeCustomizationProvider>
      )
    })
    assert.equal(
      container.querySelector('[data-testid="background-visible"]')
        ?.textContent,
      'false'
    )

    await act(async () => root.unmount())
    container.remove()
  })

  test('重置主题设置后恢复显示背景并删除偏好', async () => {
    document.cookie = `${THEME_COOKIE_KEYS.backgroundVisible}=false; path=/`
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <ThemeCustomizationProvider>
          <BackgroundPreferenceProbe />
        </ThemeCustomizationProvider>
      )
    })
    await act(async () => {
      container
        .querySelector<HTMLButtonElement>('[data-testid="reset-background"]')
        ?.click()
    })

    assert.equal(
      container.querySelector('[data-testid="background-visible"]')
        ?.textContent,
      'true'
    )
    assert.doesNotMatch(document.cookie, /theme_background_visible=/)

    await act(async () => root.unmount())
    container.remove()
  })
})
