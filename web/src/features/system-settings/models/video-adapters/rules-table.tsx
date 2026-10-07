import type {
  ColumnDef,
  OnChangeFn,
  PaginationState,
} from '@tanstack/react-table'
import { Copy, Pencil, Trash2 } from 'lucide-react'
import { useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DataTablePagination,
  DataTableView,
  TruncatedCell,
  useDataTable,
} from '@/components/data-table'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { useMediaQuery } from '@/hooks'

import type { AdapterRule, AdapterTemplate } from './index'

interface AdapterRulesTableProps {
  rules: AdapterRule[]
  channels: { id: number; name: string }[]
  templates: AdapterTemplate[]
  pagination: PaginationState
  onPaginationChange: OnChangeFn<PaginationState>
  disabled: boolean
  onEdit: (rule: AdapterRule) => void
  onDuplicate: (rule: AdapterRule) => void
  onDelete: (rule: AdapterRule) => void
  onEnabledChange: (rule: AdapterRule, enabled: boolean) => void
}

export function AdapterRulesTable(props: AdapterRulesTableProps) {
  const { t } = useTranslation()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const columns = useMemo<ColumnDef<AdapterRule>[]>(
    () => [
      {
        id: 'channels',
        header: t('Applicable channels'),
        size: 240,
        cell: ({ row }) => (
          <TruncatedCell>
            {row.original.channel_ids
              .map((id) => {
                const channel = props.channels.find((item) => item.id === id)
                return channel ? `${channel.name} · ${id}` : `#${id}`
              })
              .join('，') || t('No channel selected')}
          </TruncatedCell>
        ),
      },
      {
        id: 'models',
        header: t('Model'),
        size: 240,
        cell: ({ row }) => (
          <TruncatedCell>
            {row.original.models.join('，') || t('Channel default')}
          </TruncatedCell>
        ),
      },
      {
        id: 'template',
        header: t('Protocol template'),
        size: 230,
        cell: ({ row }) => (
          <TruncatedCell>
            {props.templates.find(
              (item) => item.id === row.original.template_id
            )?.name ||
              row.original.template_id ||
              '-'}
          </TruncatedCell>
        ),
      },
      {
        id: 'status',
        header: t('Status'),
        size: 140,
        cell: ({ row }) => (
          <div className='flex items-center gap-2'>
            <Switch
              size='sm'
              aria-label={t('Enabled')}
              checked={row.original.enabled}
              disabled={props.disabled}
              onCheckedChange={(enabled) =>
                props.onEnabledChange(row.original, enabled)
              }
            />
            <span
              className={
                row.original.enabled
                  ? 'text-emerald-600 dark:text-emerald-400'
                  : 'text-muted-foreground'
              }
            >
              {row.original.enabled ? t('Enabled') : t('Disabled')}
            </span>
          </div>
        ),
      },
      {
        id: 'actions',
        header: t('Actions'),
        size: 120,
        cell: ({ row }) => (
          <div className='flex items-center gap-1'>
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon-sm'
                    aria-label={t('Edit')}
                    disabled={props.disabled}
                    onClick={() => props.onEdit(row.original)}
                  />
                }
              >
                <Pencil />
              </TooltipTrigger>
              <TooltipContent>{t('Edit')}</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon-sm'
                    aria-label={t('Duplicate')}
                    disabled={props.disabled}
                    onClick={() => props.onDuplicate(row.original)}
                  />
                }
              >
                <Copy />
              </TooltipTrigger>
              <TooltipContent>{t('Duplicate')}</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon-sm'
                    className='text-destructive'
                    aria-label={t('Delete')}
                    disabled={props.disabled}
                    onClick={() => props.onDelete(row.original)}
                  />
                }
              >
                <Trash2 />
              </TooltipTrigger>
              <TooltipContent>{t('Delete')}</TooltipContent>
            </Tooltip>
          </div>
        ),
      },
    ],
    [props, t]
  )
  const { table } = useDataTable({
    data: props.rules,
    columns,
    getRowId: (rule) => rule.id,
    pagination: props.pagination,
    onPaginationChange: props.onPaginationChange,
    autoResetPageIndex: false,
    enableSorting: false,
    withFilteredRowModel: false,
    withSortedRowModel: false,
    withFacetedRowModel: false,
  })
  const pageCount = table.getPageCount()
  const pageIndex = props.pagination.pageIndex
  const onPaginationChange = props.onPaginationChange
  useEffect(() => {
    // 搜索或删除后收敛到有效页码；修改行内容不重置分页，也不裁剪完整草稿。
    if (pageIndex >= Math.max(1, pageCount)) {
      onPaginationChange((previous) => ({
        ...previous,
        pageIndex: Math.max(0, pageCount - 1),
      }))
    }
  }, [pageCount, pageIndex, onPaginationChange])

  return (
    <div className='min-w-0 space-y-3'>
      <DataTableView
        table={table}
        splitHeader
        containerProps={{ role: 'region', 'aria-label': t('Video adapters') }}
        containerClassName='h-[min(36rem,60dvh)] w-full min-w-0'
        tableContainerClassName='h-full min-h-0'
        bodyContainerClassName='[scrollbar-gutter:stable]'
        tableClassName='min-w-[48rem] table-fixed'
        tableHeaderClassName='[background-color:var(--table-header)]'
        pinnedColumns={[{ columnId: 'actions', side: 'right' }]}
        emptyContent={t(
          'No matching adapter rules. Add a row to bind a template.'
        )}
        emptyCellClassName='h-24 text-center whitespace-normal'
      />
      <DataTablePagination table={table} compact={isMobile} />
    </div>
  )
}
