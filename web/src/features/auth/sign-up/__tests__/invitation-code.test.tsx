import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { createRegisterFormSchema } from '@/features/auth/constants'
import type { SystemStatus } from '@/features/auth/types'
import { api } from '@/lib/api'
import { STATUS_QUERY_KEY } from '@/lib/status-query'

import { SignUpForm } from '../components/sign-up-form'

const clients: QueryClient[] = []

async function renderRegistration(status: SystemStatus = {}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  client.setQueryData(STATUS_QUERY_KEY, status)
  const root = createRootRoute({ component: Outlet })
  const registerRoute = createRoute({
    getParentRoute: () => root,
    path: '/register',
    component: SignUpForm,
  })
  const signInRoute = createRoute({
    getParentRoute: () => root,
    path: '/sign-in',
    component: () => <h1>Sign-in page</h1>,
  })
  const router = createRouter({
    routeTree: root.addChildren([registerRoute, signInRoute]),
    history: createMemoryHistory({ initialEntries: ['/register'] }),
  })
  await router.load()
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  await screen.findByRole('button', { name: 'Create account' })
  return client
}

async function fillAccount() {
  const user = userEvent.setup()
  await user.type(screen.getByLabelText('Username'), 'new-user')
  await user.type(screen.getByLabelText('Password'), 'secure-password123')
  await user.type(
    screen.getByLabelText('Confirm password'),
    'secure-password123'
  )
  return user
}

beforeEach(() => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => undefined)
  localStorage.clear()
  sessionStorage.clear()
  window.history.replaceState(null, '', '/register')
  vi.spyOn(api, 'post').mockResolvedValue({ data: { success: true } })
})

afterEach(() => {
  for (const client of clients.splice(0)) client.clear()
  localStorage.clear()
  sessionStorage.clear()
  window.history.replaceState(null, '', '/')
})

describe('注册邀请码规则', () => {
  it.each([{}, { register_invite_code_required: true }])(
    '缺省或开启必填时，空邀请码不能提交：%j',
    async (status) => {
      await renderRegistration(status)
      const user = await fillAccount()
      const input = screen.getByLabelText('Invitation Code')
      expect(input).toHaveAttribute('aria-required', 'true')
      await user.click(screen.getByRole('button', { name: 'Create account' }))
      await waitFor(() => expect(input).toHaveAttribute('aria-invalid', 'true'))
      expect(api.post).not.toHaveBeenCalled()
    }
  )

  it('关闭必填后，显示选填并允许不填邀请码完成注册', async () => {
    await renderRegistration({ register_invite_code_required: false })
    const user = await fillAccount()
    expect(screen.getByLabelText('Invitation Code (optional)')).toHaveAttribute(
      'aria-required',
      'false'
    )
    await user.click(screen.getByRole('button', { name: 'Create account' }))
    await screen.findByRole('heading', { name: 'Sign-in page' })
    expect(api.post).toHaveBeenCalledWith(
      '/api/user/register',
      expect.objectContaining({ username: 'new-user', aff_code: '' }),
      expect.anything()
    )
  })

  it('选填时保留链接的邀请码，用户清空后不再提交缓存邀请码', async () => {
    window.history.replaceState(null, '', '/register?aff=LINKCODE')
    await renderRegistration({ register_invite_code_required: false })
    const user = await fillAccount()
    const input = screen.getByLabelText('Invitation Code (optional)')
    expect(input).toHaveValue('LINKCODE')
    await user.clear(input)
    await user.click(screen.getByRole('button', { name: 'Create account' }))
    await screen.findByRole('heading', { name: 'Sign-in page' })
    expect(api.post).toHaveBeenCalledWith(
      '/api/user/register',
      expect.objectContaining({ aff_code: '' }),
      expect.anything()
    )
    expect(localStorage.getItem('aff')).toBe('')
  })

  it('设置刷新为选填后，已打开的注册表单采用新校验规则', async () => {
    const client = await renderRegistration({
      register_invite_code_required: true,
    })
    const user = await fillAccount()
    await user.click(screen.getByRole('button', { name: 'Create account' }))
    await waitFor(() =>
      expect(screen.getByLabelText('Invitation Code')).toHaveAttribute(
        'aria-invalid',
        'true'
      )
    )
    act(() =>
      client.setQueryData(STATUS_QUERY_KEY, {
        register_invite_code_required: false,
      })
    )
    await screen.findByLabelText('Invitation Code (optional)')
    await user.click(screen.getByRole('button', { name: 'Create account' }))
    await screen.findByRole('heading', { name: 'Sign-in page' })
  })

  it.each([true, false])(
    '必填设置为 %s 时，非空邀请码都原样提交后端校验',
    async (required) => {
      await renderRegistration({ register_invite_code_required: required })
      const user = await fillAccount()
      await user.type(screen.getByLabelText(/^Invitation Code/), ' VALID ')
      await user.click(screen.getByRole('button', { name: 'Create account' }))
      await screen.findByRole('heading', { name: 'Sign-in page' })
      expect(api.post).toHaveBeenCalledWith(
        '/api/user/register',
        expect.objectContaining({ aff_code: 'VALID' }),
        expect.anything()
      )
    }
  )

  it('选填不会放宽密码和确认密码校验，空白邀请码被规范为空', () => {
    const valid = {
      username: 'user',
      password: 'secure-password123',
      confirmPassword: 'secure-password123',
      affCode: '   ',
    }
    const schema = createRegisterFormSchema(false)
    expect(schema.parse(valid).affCode).toBe('')
    expect(schema.safeParse({ ...valid, password: 'short' }).success).toBe(
      false
    )
    expect(
      schema.safeParse({ ...valid, confirmPassword: 'different' }).success
    ).toBe(false)
    expect(createRegisterFormSchema().safeParse(valid).success).toBe(false)
  })
})
