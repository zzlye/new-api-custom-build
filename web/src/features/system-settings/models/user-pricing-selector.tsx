import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  getConfiguredPricingUsers,
  type PricingUser,
} from '@/features/model-pricing/user-pricing-api'
import { searchUsers } from '@/features/users/api'
import { useDebounce } from '@/hooks/use-debounce'
import { requireServerSuccess } from '@/lib/server-error-message'

export function UserPricingSelector(props: {
  user: PricingUser | null
  disabled: boolean
  onSelect: (user: PricingUser) => void
}) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const [configuredOnly, setConfiguredOnly] = useState(false)
  const keyword = useDebounce(search.trim(), 250)
  const query = useQuery({
    queryKey: ['user-pricing-users', configuredOnly, keyword, page],
    refetchOnWindowFocus: false,
    queryFn: async (): Promise<{ items: PricingUser[]; total: number }> => {
      if (configuredOnly) return getConfiguredPricingUsers(keyword, page)
      const response = await searchUsers({ keyword, p: page, page_size: 20 })
      requireServerSuccess(response)
      return response.data ?? { items: [], total: 0 }
    },
  })

  useEffect(() => {
    // 删除末页最后一个用户的规则后，回到仍有数据的页码，保留当前编辑对象。
    if (
      query.isSuccess &&
      page > Math.max(1, Math.ceil(query.data.total / 20))
    ) {
      setPage(Math.max(1, Math.ceil(query.data.total / 20)))
    }
  }, [page, query.isSuccess, query.data])

  return (
    <div className='flex min-w-0 flex-col gap-3'>
      <div className='flex gap-2' role='group' aria-label={t('Pricing users')}>
        {[false, true].map((configured) => (
          <Button
            key={String(configured)}
            size='sm'
            variant={configuredOnly === configured ? 'secondary' : 'outline'}
            className='min-w-0 flex-1'
            aria-pressed={configuredOnly === configured}
            onClick={() => {
              setConfiguredOnly(configured)
              setPage(1)
            }}
          >
            {configured ? t('Configured pricing users') : t('All users')}
          </Button>
        ))}
      </div>
      <Input
        aria-label={t('Search users by name or ID')}
        placeholder={t('Search users by name or ID')}
        value={search}
        onChange={(event) => {
          setSearch(event.target.value)
          setPage(1)
        }}
      />
      {props.user && (
        <p className='text-sm font-medium break-all'>
          {t('Selected user')}: {props.user.username} (#{props.user.id})
        </p>
      )}
      {query.isPending && <LoadingState />}
      {query.isError && (
        <ErrorState
          description={query.error.message}
          onRetry={() => void query.refetch()}
        />
      )}
      {query.isSuccess && (
        <>
          <ScrollArea className='h-52 rounded-md border'>
            <div className='flex flex-col gap-1 p-2' aria-label={t('Users')}>
              {query.data?.items.map((user) => (
                <Button
                  key={user.id}
                  variant={props.user?.id === user.id ? 'secondary' : 'ghost'}
                  className='h-auto flex-col items-start text-left break-all whitespace-normal'
                  aria-pressed={props.user?.id === user.id}
                  disabled={props.disabled}
                  onClick={() => props.onSelect(user)}
                >
                  <span>
                    #{user.id} · {user.username}
                    {user.display_name && user.display_name !== user.username
                      ? ` · ${user.display_name}`
                      : ''}
                  </span>
                  {user.model_count !== undefined && (
                    <span className='text-muted-foreground text-xs'>
                      {t('{{count}} models configured', {
                        count: user.model_count,
                      })}
                    </span>
                  )}
                </Button>
              ))}
              {!query.data?.items.length && (
                <p className='text-muted-foreground p-3 text-sm'>
                  {configuredOnly
                    ? t('No matching users with configured prices')
                    : t('No users found')}
                </p>
              )}
            </div>
          </ScrollArea>
          <div className='flex items-center justify-between gap-2'>
            <Button
              size='sm'
              variant='outline'
              disabled={page === 1 || query.isFetching}
              onClick={() => setPage(page - 1)}
            >
              {t('Previous')}
            </Button>
            <span className='text-sm'>{page}</span>
            <Button
              size='sm'
              variant='outline'
              disabled={
                page * 20 >= (query.data?.total ?? 0) || query.isFetching
              }
              onClick={() => setPage(page + 1)}
            >
              {t('Next')}
            </Button>
          </div>
        </>
      )}
    </div>
  )
}
