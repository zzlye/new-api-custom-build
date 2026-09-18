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
import { after, afterEach, test } from 'node:test'

import { Window } from 'happy-dom'

import type {
  ModelPricingEditorPanelHandle,
  ModelRatioData,
} from '../model-pricing-sheet'

// 使用真实编辑器及保存入口，覆盖价格回填和模式切换，而非复制组件内部逻辑。
const dom = new Window()
for (const key of [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLInputElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'CustomEvent',
  'MutationObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: dom[key],
  })
}
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const { act, createRef } = await import('react')
const { createRoot } = await import('react-dom/client')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { ModelPricingEditorPanel } = await import('../model-pricing-sheet')
const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        'Fixed price': '固定价格',
        'By resolution': '按分辨率',
      },
    },
  },
})

const container = document.createElement('div')
document.body.append(container)
let root = createRoot(container)
const ref = createRef<ModelPricingEditorPanelHandle>()
const expression =
  'param("resolution") == "1080p" ? 0.75 : param("resolution") == "720p" ? 0.4 : 0.23'
const fixture: ModelRatioData = {
  name: 'video-test',
  billingMode: 'per-second',
  price: '0.4',
  billingExpr: expression,
}

afterEach(async () => {
  await act(async () => root.unmount())
  root = createRoot(container)
})
after(async () => {
  await act(async () => root.unmount())
  dom.close()
})
async function render(data = fixture) {
  await act(async () =>
    root.render(
      <I18nextProvider i18n={i18n}>
        <ModelPricingEditorPanel ref={ref} editData={data} />
      </I18nextProvider>
    )
  )
}
function input(resolution: string) {
  const label = [...container.querySelectorAll('label')].find(
    (el) => el.textContent === resolution
  )
  const field = label?.closest('[data-slot="field"]')?.querySelector('input')
  assert.ok(field)
  return field
}
async function change(resolution: string, value: string) {
  await act(async () => {
    const field = input(resolution)
    const setter = Object.getOwnPropertyDescriptor(
      dom.HTMLInputElement.prototype,
      'value'
    )?.set
    assert.ok(setter)
    setter.call(field, value)
    field.dispatchEvent(
      new dom.Event('input', { bubbles: true }) as unknown as Event
    )
  })
}
async function click(label: string) {
  const button = [...container.querySelectorAll('button')].find(
    (el) => el.textContent === label
  )
  assert.ok(button)
  await act(async () => button.click())
}
async function save() {
  let data: ModelRatioData | null = null
  await act(async () => {
    assert.ok(ref.current)
    data = await ref.current.commitDraft()
  })
  return data as ModelRatioData | null
}

test('重新编辑并修改1080p时保留480p原价', async () => {
  await render()
  assert.equal(input('480p').value, '0.23')
  await change('1080p', '0.8')
  assert.equal((await save())?.billingExpr, expression.replace('0.75', '0.8'))
})
test('填写三个价格后无需填写隐藏的固定价格即可保存', async () => {
  await render({ ...fixture, price: '', billingExpr: '' })
  await click('按分辨率')
  await change('480p', '0.23')
  await change('720p', '0.4')
  await change('1080p', '0.75')
  assert.equal((await save())?.billingExpr, expression)
  assert.equal((await save())?.price, '0.4')
})
test('切换固定再切回分辨率时保存原有三档价格', async () => {
  await render()
  await click('固定价格')
  assert.equal((await save())?.billingExpr, '')
  await click('按分辨率')
  assert.equal((await save())?.billingExpr, expression)
})
test('分辨率价格缺项时阻止保存并显示错误', async () => {
  await render()
  await change('720p', '')
  assert.equal(await save(), null)
  assert.ok(container.querySelector('[role="alert"]'))
})
test('分辨率价格为零时阻止保存', async () => {
  await render()
  await change('1080p', '0')
  assert.equal(await save(), null)
})

test('填写完整后清除错误并同步价格预览', async () => {
  await render()
  await change('480p', '')
  assert.equal(await save(), null)
  assert.equal(input('480p').getAttribute('aria-invalid'), 'true')
  await change('480p', '0.3')
  assert.equal(container.querySelector('[role="alert"]'), null)
  assert.equal(input('480p').getAttribute('aria-invalid'), 'false')
  assert.ok(container.querySelector('aside')?.textContent?.includes('$0.3'))
  assert.equal(
    (await save())?.billingExpr,
    expression.replace(': 0.23', ': 0.3')
  )
})

test('选择固定价格时仅显示固定输入且清除保存的分辨率规则', async () => {
  await render()
  await click('固定价格')
  const buttons = [...container.querySelectorAll('button')]
  assert.equal(
    buttons
      .find((button) => button.textContent === '固定价格')
      ?.getAttribute('aria-pressed'),
    'true'
  )
  assert.equal(
    buttons
      .find((button) => button.textContent === '按分辨率')
      ?.getAttribute('aria-pressed'),
    'false'
  )
  assert.equal(
    container.querySelector('input[aria-label="480p Price per second"]'),
    null
  )
  const saved = await save()
  assert.equal(saved?.price, '0.4')
  assert.equal(saved?.billingExpr, '')
})

test('切换不同模型后回填对应价格且清除前一个模型错误', async () => {
  await render()
  await change('480p', '')
  assert.equal(await save(), null)
  await render({
    ...fixture,
    name: 'video-other',
    billingExpr: expression.replace('0.23', '0.12'),
  })
  assert.equal(input('480p').value, '0.12')
  assert.equal(container.querySelector('[role="alert"]'), null)
  assert.equal((await save())?.name, 'video-other')
})
