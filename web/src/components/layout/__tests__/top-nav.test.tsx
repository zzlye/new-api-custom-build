import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it } from 'vitest'

import { useTopNavLinks } from '@/hooks/use-top-nav-links'
import { STATUS_QUERY_KEY } from '@/lib/status-query'
import { useAuthStore } from '@/stores/auth-store'

import { TopNav } from '../components/top-nav'

const clients: QueryClient[] = []
afterEach(() => {
  clients.forEach((client) => client.clear())
  clients.length = 0
  useAuthStore.getState().auth.reset()
})

it('顶部导航在收起的菜单中保留抽奖入口并可跳转独立页面', async () => {
  const client = new QueryClient()
  clients.push(client)
  client.setQueryData(STATUS_QUERY_KEY, {})
  useAuthStore.getState().auth.setUser({ id: 1, username: 'viewer', role: 1 })
  function Navigation() {
    const links = useTopNavLinks()
    return <TopNav links={links} />
  }
  const route = createRootRoute({ component: Navigation })
  const router = createRouter({
    routeTree: route,
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  const user = userEvent.setup()
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  await user.click(await screen.findByRole('button', { name: 'Navigation' }))
  const entry = await screen.findByRole('menuitem', { name: 'Lucky draw' })
  expect(entry).toHaveAttribute('href', '/lottery')
  await user.click(entry)
  await waitFor(() => expect(router.state.location.pathname).toBe('/lottery'))
})
