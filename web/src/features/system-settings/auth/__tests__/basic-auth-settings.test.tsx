import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance, type i18n } from 'i18next'
import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import zh from '@/i18n/locales/zh.json'
import { api } from '@/lib/api'
import { STATUS_QUERY_KEY } from '@/lib/status-query'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { BasicAuthSection } from '../basic-auth-section'

let client: QueryClient
let testI18n: i18n

function Fixture(props: { required?: boolean }) {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)
  return (
    <I18nextProvider i18n={testI18n}>
      <QueryClientProvider client={client}>
        <div ref={setContainer} />
        <SettingsPageProvider actionsContainer={container}>
          <BasicAuthSection
            defaultValues={{
              PasswordLoginEnabled: true,
              PasswordRegisterEnabled: true,
              RegisterEnabled: true,
              RegisterInviteCodeRequired: props.required ?? true,
              EmailVerificationEnabled: false,
              EmailDomainRestrictionEnabled: false,
              EmailAliasRestrictionEnabled: false,
              EmailDomainWhitelist: '',
            }}
          />
        </SettingsPageProvider>
      </QueryClientProvider>
    </I18nextProvider>
  )
}

beforeEach(async () => {
  client = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
  client.setQueryData(STATUS_QUERY_KEY, { register_invite_code_required: true })
  localStorage.setItem('status', '{"register_invite_code_required":true}')
  testI18n = createInstance()
  await testI18n.init({ lng: 'zh', resources: { zh } })
  vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
})

afterEach(() => {
  client.clear()
  localStorage.clear()
})

describe('基本身份验证邀请码设置', () => {
  it.each([true, false])(
    '当前必填值 %s 可切换保存，且不改变其他认证设置',
    async (required) => {
      const user = userEvent.setup()
      render(<Fixture required={required} />)
      const toggle = screen.getByRole('switch', { name: '注册必须填写邀请码' })
      expect(toggle).toHaveAttribute('aria-checked', String(required))
      toggle.focus()
      await user.keyboard(' ')
      expect(toggle).toHaveAttribute('aria-checked', String(!required))
      await user.click(
        screen.getByRole('button', { name: testI18n.t('Save Changes') })
      )
      await waitFor(() =>
        expect(api.put).toHaveBeenCalledWith('/api/option/', {
          key: 'RegisterInviteCodeRequired',
          value: !required,
        })
      )
      expect(api.put).toHaveBeenCalledTimes(1)
      await waitFor(() =>
        expect(client.getQueryState(STATUS_QUERY_KEY)?.isInvalidated).toBe(true)
      )
      expect(localStorage.getItem('status')).toBeNull()
    }
  )

  it('保存失败时保留原公开规则，不清除状态缓存', async () => {
    vi.mocked(api.put).mockResolvedValue({
      data: { success: false, message: '保存失败' },
    })
    const user = userEvent.setup()
    render(<Fixture />)
    await user.click(screen.getByRole('switch', { name: '注册必须填写邀请码' }))
    await user.click(
      screen.getByRole('button', { name: testI18n.t('Save Changes') })
    )
    await waitFor(() => expect(client.isMutating()).toBe(0))
    expect(api.put).toHaveBeenCalledTimes(1)
    expect(client.getQueryState(STATUS_QUERY_KEY)?.isInvalidated).toBe(false)
    expect(localStorage.getItem('status')).not.toBeNull()
  })
})
