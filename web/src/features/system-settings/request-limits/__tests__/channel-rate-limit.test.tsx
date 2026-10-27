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
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { RateLimitDialog } from '../rate-limit-dialog'
import { RateLimitVisualEditor } from '../rate-limit-visual-editor'

describe('渠道速率限制', () => {
  it('新增渠道规则允许成功数为零，并保留分组与模型规则', async () => {
    const user = userEvent.setup()
    const groupChange = vi.fn()
    const modelChange = vi.fn()
    const channelChange = vi.fn()
    render(
      <RateLimitVisualEditor
        groupValue='{"vip":[100,20]}'
        modelValue='{"gpt-test":[10,5]}'
        channelValue='{}'
        onGroupChange={groupChange}
        onModelChange={modelChange}
        onChannelChange={channelChange}
      />
    )
    await user.click(screen.getByRole('button', { name: 'Add' }))
    const dialog = within(screen.getByRole('dialog'))
    await user.click(dialog.getByRole('button', { name: 'Channel' }))
    expect(dialog.getByRole('button', { name: 'Channel' })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    await user.type(dialog.getByRole('textbox', { name: 'Target' }), '175')
    fireEvent.change(
      dialog.getByRole('spinbutton', {
        name: 'Max Requests (including failures)',
      }),
      { target: { value: '2' } }
    )
    fireEvent.change(
      dialog.getByRole('spinbutton', { name: 'Max Successful Requests' }),
      { target: { value: '0' } }
    )
    await user.click(dialog.getByRole('button', { name: 'Add' }))
    await waitFor(() => expect(channelChange).toHaveBeenCalled())
    expect(JSON.parse(channelChange.mock.calls[0][0])).toEqual({
      '175': [2, 0],
    })
    expect(groupChange).not.toHaveBeenCalled()
    expect(modelChange).not.toHaveBeenCalled()
  })

  it('渠道编号非法时显示校验错误，且不能以零成功数保存旧类型', async () => {
    const user = userEvent.setup()
    const save = vi.fn(() => null)
    render(
      <RateLimitDialog
        open
        onOpenChange={vi.fn()}
        onSave={save}
        editData={{
          targetType: 'channel',
          target: '0175',
          maxRequests: 2,
          maxSuccess: 0,
        }}
      />
    )
    await user.click(screen.getByRole('button', { name: 'Update' }))
    expect(await screen.findByText('Enter a valid channel ID.')).toBeVisible()
    expect(save).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Model' }))
    // 表单提交直接覆盖原生数字校验，验证服务端同口径的表单规则仍然生效。
    const form = screen.getByRole('dialog').querySelector('form')
    expect(form).not.toBeNull()
    if (form) fireEvent.submit(form)
    expect(await screen.findByText('Must be ≥ 1')).toBeVisible()
    expect(save).not.toHaveBeenCalled()
  })

  it('重复渠道被拒绝，已有渠道可以编辑和删除', async () => {
    const user = userEvent.setup()
    function Editor() {
      const [value, setValue] = useState('{"175":[2,0]}')
      return (
        <RateLimitVisualEditor
          groupValue='{}'
          modelValue='{}'
          channelValue={value}
          onGroupChange={vi.fn()}
          onModelChange={vi.fn()}
          onChannelChange={setValue}
        />
      )
    }
    render(<Editor />)
    await user.click(screen.getByRole('button', { name: 'Add' }))
    let dialog = within(screen.getByRole('dialog'))
    await user.click(dialog.getByRole('button', { name: 'Channel' }))
    await user.type(dialog.getByRole('textbox', { name: 'Target' }), '175')
    await user.click(dialog.getByRole('button', { name: 'Add' }))
    expect(
      await screen.findByText('This target already has a rate limit.')
    ).toBeVisible()
    await user.click(dialog.getByRole('button', { name: 'Cancel' }))
    await user.click(screen.getByRole('button', { name: 'Edit' }))
    dialog = within(screen.getByRole('dialog'))
    fireEvent.change(
      dialog.getByRole('spinbutton', {
        name: 'Max Requests (including failures)',
      }),
      { target: { value: '1' } }
    )
    await user.click(dialog.getByRole('button', { name: 'Update' }))
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    )
    expect(screen.getByRole('row', { name: /175/ })).toHaveTextContent('1')
    await user.click(screen.getByRole('button', { name: 'Open menu' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    await waitFor(() =>
      expect(screen.queryByRole('row', { name: /175/ })).not.toBeInTheDocument()
    )
  })
})
