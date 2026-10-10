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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { GlobalSettingsCard } from '../global-settings-card'

const { save } = vi.hoisted(() => ({
  save: vi.fn().mockResolvedValue(undefined),
}))
vi.mock('../../hooks/use-update-option', () => ({
  useUpdateOption: () => ({ mutateAsync: save, isPending: false }),
}))
vi.mock('@/components/json-code-editor', () => ({ JsonCodeEditor: () => null }))

// 使用实际设置表单及保存入口，验证新增开关的初始化、持久化和重新加载。
function Fixture({ enabled = false }: { enabled?: boolean }) {
  const [actions, setActions] = useState<HTMLDivElement | null>(null)
  return (
    <SettingsPageProvider actionsContainer={actions}>
      <div ref={setActions} />
      <GlobalSettingsCard
        defaultValues={{
          global: {
            pass_through_request_enabled: false,
            gemini_image_url_enabled: enabled,
            thinking_model_blacklist: '[]',
            chat_completions_to_responses_policy: '{}',
          },
          general_setting: {
            ping_interval_enabled: false,
            ping_interval_seconds: 60,
          },
        }}
      />
    </SettingsPageProvider>
  )
}

describe('Gemini image URL setting', () => {
  it('defaults to off and saves only the changed option', async () => {
    const user = userEvent.setup()
    render(<Fixture />)
    const toggle = screen.getByRole('switch', {
      name: 'Enable Gemini Image URL Responses',
    })
    expect(toggle).not.toBeChecked()
    await user.click(toggle)
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() =>
      expect(save).toHaveBeenCalledExactlyOnceWith({
        key: 'global.gemini_image_url_enabled',
        value: true,
      })
    )
  })

  it('restores an enabled option and allows disabling it', async () => {
    const user = userEvent.setup()
    render(<Fixture enabled />)
    const toggle = screen.getByRole('switch', {
      name: 'Enable Gemini Image URL Responses',
    })
    expect(toggle).toBeChecked()
    await user.click(toggle)
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() =>
      expect(save).toHaveBeenCalledExactlyOnceWith({
        key: 'global.gemini_image_url_enabled',
        value: false,
      })
    )
  })

  it('reloads saved values without changing other settings', async () => {
    const view = render(<Fixture />)
    view.rerender(<Fixture enabled />)
    expect(
      screen.getByRole('switch', { name: 'Enable Gemini Image URL Responses' })
    ).toBeChecked()
    expect(
      screen.getByRole('switch', { name: 'Enable Request Passthrough' })
    ).not.toBeChecked()
    await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
    expect(save).not.toHaveBeenCalled()
  })
})
