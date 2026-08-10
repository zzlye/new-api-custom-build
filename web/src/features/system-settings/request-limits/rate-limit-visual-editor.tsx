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
import { Plus, Search } from 'lucide-react'
import { useState, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { StaticRowActions } from '@/components/data-table/static/static-row-actions'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

import { safeJsonParseWithValidation } from '../utils/json-parser'
import { isObjectRecord } from '../utils/json-validators'
import { RateLimitDialog, type RateLimitEntryData } from './rate-limit-dialog'

type RateLimitVisualEditorProps = {
  groupValue: string
  modelValue: string
  onGroupChange: (value: string) => void
  onModelChange: (value: string) => void
}

type RateLimitEntry = RateLimitEntryData

function parseRateLimitMap(value: string): Record<string, unknown> {
  return safeJsonParseWithValidation<Record<string, unknown>>(value, {
    fallback: {},
    validator: isObjectRecord,
    validatorMessage: 'Rate limits must be a JSON object',
    context: 'rate limits',
  })
}

function parseRateLimitEntries(
  value: string,
  targetType: RateLimitEntry['targetType']
): RateLimitEntry[] {
  if (!value || value.trim() === '') return []

  return Object.entries(parseRateLimitMap(value))
    .map(([target, limits]) => {
      if (
        Array.isArray(limits) &&
        limits.length === 2 &&
        typeof limits[0] === 'number' &&
        typeof limits[1] === 'number'
      ) {
        return {
          targetType,
          target,
          maxRequests: limits[0],
          maxSuccess: limits[1],
        }
      }
      return null
    })
    .filter((item): item is RateLimitEntry => item !== null)
}

export function RateLimitVisualEditor({
  groupValue,
  modelValue,
  onGroupChange,
  onModelChange,
}: RateLimitVisualEditorProps) {
  const { t } = useTranslation()
  const [searchText, setSearchText] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editData, setEditData] = useState<RateLimitEntry | null>(null)

  const rateLimits = useMemo(() => {
    // 两类后端配置在视觉模式下合并展示，保存时仍分别写回原字段。
    return [
      ...parseRateLimitEntries(groupValue, 'group'),
      ...parseRateLimitEntries(modelValue, 'model'),
    ]
  }, [groupValue, modelValue])

  const filteredRateLimits = useMemo(() => {
    if (!searchText) return rateLimits
    const lowerSearch = searchText.toLowerCase()
    return rateLimits.filter((limit) =>
      limit.target.toLowerCase().includes(lowerSearch)
    )
  }, [rateLimits, searchText])

  const handleSave = (data: RateLimitEntryData): string | null => {
    const normalizedData = { ...data, target: data.target.trim() }
    const duplicate = rateLimits.some(
      (limit) =>
        limit.targetType === normalizedData.targetType &&
        limit.target === normalizedData.target &&
        (!editData ||
          limit.targetType !== editData.targetType ||
          limit.target !== editData.target)
    )
    if (duplicate) {
      return t('This target already has a rate limit.')
    }

    const maps = {
      group: parseRateLimitMap(groupValue),
      model: parseRateLimitMap(modelValue),
    }
    const changedTypes = new Set<RateLimitEntry['targetType']>()

    if (editData) {
      delete maps[editData.targetType][editData.target]
      changedTypes.add(editData.targetType)
    }

    maps[normalizedData.targetType][normalizedData.target] = [
      normalizedData.maxRequests,
      normalizedData.maxSuccess,
    ]
    changedTypes.add(normalizedData.targetType)

    if (changedTypes.has('group')) {
      onGroupChange(JSON.stringify(maps.group, null, 2))
    }
    if (changedTypes.has('model')) {
      onModelChange(JSON.stringify(maps.model, null, 2))
    }

    return null
  }

  const handleDelete = (limit: RateLimitEntry) => {
    const value = limit.targetType === 'group' ? groupValue : modelValue
    const parsed = parseRateLimitMap(value)

    delete parsed[limit.target]

    const nextValue = JSON.stringify(parsed, null, 2)
    if (limit.targetType === 'group') {
      onGroupChange(nextValue)
    } else {
      onModelChange(nextValue)
    }
  }

  const handleEdit = (limit: RateLimitEntry) => {
    setEditData(limit)
    setDialogOpen(true)
  }

  const handleAdd = () => {
    setEditData(null)
    setDialogOpen(true)
  }

  return (
    <div className='min-w-0 space-y-4'>
      <div className='flex min-w-0 flex-col gap-3 sm:flex-row sm:items-center'>
        <div className='relative min-w-0 flex-1'>
          <Search className='text-muted-foreground absolute top-2.5 left-2.5 h-4 w-4' />
          <Input
            placeholder={t('Search targets...')}
            value={searchText}
            onChange={(e) => setSearchText(e.target.value)}
            className='pl-9'
          />
        </div>
        <Button
          type='button'
          className='self-end sm:self-auto'
          onClick={handleAdd}
        >
          <Plus className='mr-2 h-4 w-4' />
          {t('Add')}
        </Button>
      </div>

      <StaticDataTable
        className='max-w-full min-w-0'
        tableClassName='min-w-[680px]'
        data={filteredRateLimits}
        getRowKey={(limit) => `${limit.targetType}:${limit.target}`}
        emptyContent={
          searchText
            ? t('No targets match your search')
            : t(
                'No target-specific rate limits configured. Click "Add" to get started.'
              )
        }
        columns={[
          {
            id: 'type',
            header: t('Type'),
            cell: (limit) => (
              <span className='bg-muted rounded-md px-2 py-1 text-xs font-medium'>
                {t(limit.targetType === 'group' ? 'Group' : 'Model')}
              </span>
            ),
          },
          {
            id: 'target',
            header: t('Target'),
            cellClassName: 'font-medium',
            cell: (limit) => limit.target,
          },
          {
            id: 'max-requests',
            header: t('Max Requests (incl. failures)'),
            className: 'text-right',
            cellClassName: 'text-right',
            cell: (limit) => (
              <span className='font-mono'>
                {limit.maxRequests === 0
                  ? t('Unlimited')
                  : limit.maxRequests.toLocaleString()}
              </span>
            ),
          },
          {
            id: 'max-success',
            header: t('Max Success'),
            className: 'text-right',
            cellClassName: 'text-right',
            cell: (limit) => (
              <span className='font-mono'>
                {limit.maxSuccess.toLocaleString()}
              </span>
            ),
          },
          {
            id: 'actions',
            header: t('Actions'),
            className: 'text-right',
            cellClassName: 'text-right',
            cell: (limit) => (
              <StaticRowActions
                editLabel={t('Edit')}
                deleteLabel={t('Delete')}
                menuLabel={t('Open menu')}
                onEdit={() => handleEdit(limit)}
                onDelete={() => handleDelete(limit)}
              />
            ),
          },
        ]}
      />

      <RateLimitDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onSave={handleSave}
        editData={editData}
      />
    </div>
  )
}
