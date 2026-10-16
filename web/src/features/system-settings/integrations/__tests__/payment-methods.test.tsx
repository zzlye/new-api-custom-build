import {
  fireEvent,
  render,
  screen,
  within,
  waitFor,
} from '@testing-library/react'
import { useState } from 'react'
import { expect, test } from 'vitest'

import { PaymentMethodsVisualEditor } from '../payment-methods-visual-editor'

function Editor() {
  const [value, setValue] = useState('[{"name":"银行卡","type":"stripe"}]')
  return (
    <>
      <PaymentMethodsVisualEditor value={value} onChange={setValue} />
      <output aria-label='保存内容'>{value}</output>
    </>
  )
}

test('旧配置自动显示收款码，改名及排序后保留独立支付类型且不重复添加', async () => {
  render(<Editor />)
  const row = screen.getByRole('row', { name: /支付宝收款码/ })
  fireEvent.click(within(row).getByRole('button', { name: 'Edit' }))
  const dialog = screen.getByRole('dialog', { name: 'Edit payment method' })
  fireEvent.change(within(dialog).getByLabelText('Name'), {
    target: { value: '西米露支付宝' },
  })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Update' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  const changed = screen.getByRole('row', { name: /西米露支付宝/ })
  fireEvent.click(within(changed).getByRole('button', { name: 'Move up' }))
  const saved = JSON.parse(
    screen.getByLabelText('保存内容').textContent || '[]'
  )
  expect(saved.map((item: { type: string }) => item.type)).toEqual([
    'alipay_receipt',
    'stripe',
  ])
  expect(saved[0]).toMatchObject({ name: '西米露支付宝', icon: 'SiAlipay' })
  fireEvent.click(screen.getByRole('button', { name: 'Templates' }))
  fireEvent.click(screen.getByRole('button', { name: 'Alipay receipt code' }))
  expect(
    JSON.parse(screen.getByLabelText('保存内容').textContent || '[]')
  ).toEqual(saved)
})
