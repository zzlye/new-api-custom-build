import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  expect,
  test,
  vi,
} from 'vitest'

import { NotificationPopover } from '@/components/notification-popover'
import { api } from '@/lib/api'
import { useNotificationStore } from '@/stores/notification-store'

import { NoticePopup } from '../index'

const timeline = [
  {
    id: 'price',
    content: 'Pricing updated',
    publishDate: '2026-10-06T11:45:33+08:00',
    extra: 'Existing tasks stay unchanged',
  },
  {
    id: 'maintenance',
    content: 'Maintenance finished',
    publishDate: '2026-10-05T20:47:21+08:00',
  },
]
const clients: QueryClient[] = []
// 仅补齐测试浏览器接口，复用真实弹窗、滚动、富文本和通知存储。
const animationDescriptor = Object.getOwnPropertyDescriptor(
  Element.prototype,
  'getAnimations'
)
beforeAll(() => {
  Object.defineProperty(Element.prototype, 'getAnimations', {
    configurable: true,
    value: () => [],
  })
})
afterAll(() => {
  if (animationDescriptor) {
    Object.defineProperty(
      Element.prototype,
      'getAnimations',
      animationDescriptor
    )
  } else Reflect.deleteProperty(Element.prototype, 'getAnimations')
})
beforeEach(() => {
  localStorage.clear()
  useNotificationStore.setState({
    closedUntilDate: null,
    lastReadNotice: '',
    readAnnouncementKeys: [],
  })
})
afterEach(() => {
  for (const client of clients.splice(0)) client.clear()
  vi.useRealTimers()
  vi.restoreAllMocks()
  localStorage.clear()
})

function setup(
  notice = 'Welcome **everyone**',
  announcements = timeline,
  enabled = true
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  client.setQueryData(['notice'], { success: true, data: notice })
  client.setQueryData(['status'], {
    announcements_enabled: enabled,
    announcements,
  })
  const view = render(
    <QueryClientProvider client={client}>
      <NoticePopup />
    </QueryClientProvider>
  )
  return { ...view, client, user: userEvent.setup() }
}

test('打开时优先时间线，通知可切换且保留完整内容与时间', async () => {
  const { user } = setup()
  const dialog = await screen.findByRole('dialog', { name: 'System Notice' })
  expect(within(dialog).getByRole('tab', { name: 'Timeline' })).toHaveAttribute(
    'aria-selected',
    'true'
  )
  expect(within(dialog).getByText('Pricing updated')).toBeVisible()
  expect(
    within(dialog).getByText('Existing tasks stay unchanged')
  ).toBeVisible()
  expect(dialog).toHaveTextContent('2026-10-06')
  await user.click(screen.getByRole('tab', { name: 'Notice' }))
  expect(screen.getByText('everyone')).toBeVisible()
  expect(screen.getByRole('tab', { name: 'Notice' })).toHaveAttribute(
    'aria-selected',
    'true'
  )
})

test('普通关闭只影响本次打开，重新打开网页再次显示且优先时间线', async () => {
  const view = setup()
  await view.user.click(screen.getAllByRole('button', { name: 'Close' })[0])
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  act(() =>
    view.client.setQueryData(['notice'], {
      success: true,
      data: 'Edited notice',
    })
  )
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  expect(useNotificationStore.getState().closedUntilDate).toBeNull()
  view.unmount()
  setup()
  expect(await screen.findByRole('dialog')).toBeVisible()
  expect(screen.getByRole('tab', { name: 'Timeline' })).toHaveAttribute(
    'aria-selected',
    'true'
  )
})

test('今日不再提醒持久化，刷新及更新公告后当天仍不弹出', async () => {
  const view = setup()
  await view.user.click(
    screen.getByRole('button', { name: "Don't show again today" })
  )
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  const stored = JSON.parse(
    localStorage.getItem('notification-storage') ?? '{}'
  )
  expect(stored.state.closedUntilDate).toBe(new Date().toDateString())
  view.unmount()
  await useNotificationStore.persist.rehydrate()
  setup('New notice', [{ ...timeline[0], content: 'New timeline item' }])
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
})

test('昨天的免打扰记录不会屏蔽今天的公告', async () => {
  const yesterday = new Date()
  yesterday.setDate(yesterday.getDate() - 1)
  useNotificationStore.getState().setClosedUntilDate(yesterday.toDateString())
  setup()
  expect(await screen.findByRole('dialog')).toBeVisible()
})

test('无时间线或关闭时间线时默认通知，两个标签仍可切换', async () => {
  const view = setup('Only notice', timeline, false)
  expect(await screen.findByText('Only notice')).toBeVisible()
  expect(screen.getByRole('tab', { name: 'Notice' })).toHaveAttribute(
    'aria-selected',
    'true'
  )
  await view.user.click(screen.getByRole('tab', { name: 'Timeline' }))
  expect(screen.getByText('No system announcements')).toBeVisible()
  expect(screen.queryByText('Pricing updated')).not.toBeInTheDocument()
})

test('只有时间线时照常弹出，切换通知展示空状态', async () => {
  const { user } = setup('')
  expect(await screen.findByText('Pricing updated')).toBeVisible()
  await user.click(screen.getByRole('tab', { name: 'Notice' }))
  expect(screen.getByText('No announcements at this time')).toBeVisible()
})

test('公告和时间线均为空时不弹空窗', () => {
  setup('   ', [])
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
})

test('数据加载完成后再弹出，接口失败时不弹空窗', async () => {
  let rejectNotice!: (reason: Error) => void
  vi.spyOn(api, 'get').mockImplementation((url) => {
    if (url === '/api/notice') {
      return new Promise((_resolve, reject) => {
        rejectNotice = reject
      })
    }
    return Promise.resolve({
      data: {
        success: true,
        data: { announcements_enabled: true, announcements: [] },
      },
    })
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <NoticePopup />
    </QueryClientProvider>
  )
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  await act(async () => rejectNotice(new Error('Network unavailable')))
  await waitFor(() =>
    expect(client.getQueryState(['notice'])?.status).toBe('error')
  )
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
})

test('按Escape关闭不设置全天免打扰', async () => {
  const { user } = setup()
  await screen.findByRole('dialog')
  await user.keyboard('{Escape}')
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(useNotificationStore.getState().closedUntilDate).toBeNull()
})

test('存储写入失败仍能关闭公告且页面继续可用', async () => {
  const { user } = setup()
  vi.spyOn(localStorage, 'setItem').mockImplementation(() => {
    throw new Error('Storage blocked')
  })
  await user.click(
    screen.getByRole('button', { name: "Don't show again today" })
  )
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
})

test('免打扰不影响右上角通知入口手动查看两个标签', async () => {
  useNotificationStore.getState().setClosedUntilDate(new Date().toDateString())
  const onTabChange = vi.fn()
  const user = userEvent.setup()
  render(
    <NotificationPopover
      open
      onOpenChange={vi.fn()}
      unreadCount={0}
      activeTab='announcements'
      onTabChange={onTabChange}
      notice='Manual notice'
      announcements={timeline}
      loading={false}
    />
  )
  expect(await screen.findByText('Pricing updated')).toBeVisible()
  await user.click(screen.getByRole('tab', { name: 'Notice' }))
  expect(onTabChange).toHaveBeenCalledWith('notice')
})
