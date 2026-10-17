import {
  focusManager,
  QueryClient,
  QueryClientProvider,
} from '@tanstack/react-query'
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
  focusManager.setFocused(true)
  useNotificationStore.setState({
    closedUntilDate: null,
    closedContentKeys: null,
    lastReadNotice: '',
    readAnnouncementKeys: [],
  })
})
afterEach(() => {
  for (const client of clients.splice(0)) client.clear()
  vi.useRealTimers()
  focusManager.setFocused(undefined)
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

test('普通关闭后新通知重新弹出，重新打开网页仍优先时间线', async () => {
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
  expect(await screen.findByRole('dialog')).toBeVisible()
  expect(screen.getByText('Edited notice')).toBeVisible()
  expect(useNotificationStore.getState().closedUntilDate).toBeNull()
  view.unmount()
  setup()
  expect(await screen.findByRole('dialog')).toBeVisible()
  expect(screen.getByRole('tab', { name: 'Timeline' })).toHaveAttribute(
    'aria-selected',
    'true'
  )
})

test('今日不再提醒持久化且只屏蔽当时的内容，新公告仍弹出', async () => {
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
  const reopened = setup()
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  act(() =>
    reopened.client.setQueryData(['status'], {
      announcements_enabled: true,
      announcements: [{ ...timeline[0], content: 'New timeline item' }],
    })
  )
  expect(await screen.findByText('New timeline item')).toBeVisible()
})

test('关闭后删除或重新排序公告不会重弹，新增公告才弹出', async () => {
  const view = setup()
  await view.user.click(screen.getByRole('tab', { name: 'Notice' }))
  await view.user.click(screen.getAllByRole('button', { name: 'Close' })[0])
  act(() =>
    view.client.setQueryData(['status'], {
      announcements_enabled: true,
      announcements: [...timeline].reverse(),
    })
  )
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  act(() =>
    view.client.setQueryData(['status'], {
      announcements_enabled: true,
      announcements: [timeline[0]],
    })
  )
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  act(() =>
    view.client.setQueryData(['status'], {
      announcements_enabled: true,
      announcements: [
        ...timeline,
        { ...timeline[0], id: 'new', content: 'New release' },
      ],
    })
  )
  expect(await screen.findByText('New release')).toBeVisible()
  expect(screen.getByRole('tab', { name: 'Timeline' })).toHaveAttribute(
    'aria-selected',
    'true'
  )
})

test('公告超过二十条时新增公告仍显示且关闭后相同内容不重弹', async () => {
  const oldItems = Array.from({ length: 20 }, (_, id) => ({
    ...timeline[1],
    id: String(id),
    content: `Old item ${id}`,
  }))
  const view = setup('', oldItems)
  await view.user.click(screen.getAllByRole('button', { name: 'Close' })[0])
  const nextStatus = {
    announcements_enabled: true,
    announcements: [
      ...oldItems,
      { ...timeline[0], id: 'latest', content: 'Latest release' },
    ],
  }
  act(() => view.client.setQueryData(['status'], nextStatus))
  expect(await screen.findByText('Latest release')).toBeVisible()
  await view.user.click(screen.getAllByRole('button', { name: 'Close' })[0])
  act(() => view.client.setQueryData(['status'], structuredClone(nextStatus)))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
})

test('昨天的免打扰记录不会屏蔽今天的公告', async () => {
  const yesterday = new Date()
  yesterday.setDate(yesterday.getDate() - 1)
  useNotificationStore.getState().setClosedUntilDate(yesterday.toDateString())
  setup()
  expect(await screen.findByRole('dialog')).toBeVisible()
})

test('不刷新页面时自动获取新公告，检查失败不重弹，卸载后停止检查', async () => {
  // 只控制轮询时钟，弹窗交互与查询通知继续使用真实浏览器计时。
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
  let failed = true
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (failed) throw new Error('Network unavailable')
    return {
      data: {
        success: true,
        data:
          url === '/api/notice'
            ? 'Welcome **everyone**'
            : {
                announcements_enabled: true,
                announcements: [
                  ...timeline,
                  {
                    ...timeline[0],
                    id: 'live',
                    content: 'Published while open',
                  },
                ],
              },
      },
    }
  })
  const view = setup()
  await view.user.click(screen.getAllByRole('button', { name: 'Close' })[0])
  await act(async () => {
    await vi.advanceTimersByTimeAsync(60_000)
  })
  await waitFor(() =>
    expect(view.client.getQueryState(['status'])?.fetchStatus).toBe('idle')
  )
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  failed = false
  await act(async () => {
    await vi.advanceTimersByTimeAsync(60_000)
  })
  expect(await screen.findByText('Published while open')).toBeVisible()
  view.unmount()
  get.mockClear()
  await act(async () => {
    await vi.advanceTimersByTimeAsync(60_000)
  })
  expect(get).not.toHaveBeenCalled()
})

test('切回标签页时检查新通知，即使缓存尚未过期也会弹出', async () => {
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data:
        url === '/api/notice'
          ? 'Notice after focus'
          : {
              announcements_enabled: true,
              announcements: timeline,
            },
    },
  }))
  const view = setup()
  await view.user.click(
    screen.getByRole('button', { name: "Don't show again today" })
  )
  act(() => focusManager.setFocused(false))
  await act(async () => focusManager.setFocused(true))
  expect(await screen.findByText('Notice after focus')).toBeVisible()
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
