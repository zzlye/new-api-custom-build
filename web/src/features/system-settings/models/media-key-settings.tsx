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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { getMediaKeyConfig } from '@/features/keys/api'
import { AutoGroupOrderEditor } from '@/features/keys/components/auto-group-order-editor'
import { getGroups } from '@/features/users/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

export function MediaKeySettings() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const update = useUpdateOption()
  const [draft, setDraft] = useState<string[] | null>(null)
  const config = useQuery({
    queryKey: ['media-key-config'],
    queryFn: async () => requireServerSuccess(await getMediaKeyConfig()),
    staleTime: 0,
  })
  const groups = useQuery({
    queryKey: ['media-key-admin-groups'],
    queryFn: async () => requireServerSuccess(await getGroups()),
  })
  const available = (groups.data?.data ?? []).filter(
    (group) => group !== 'auto'
  )
  const value = draft ?? config.data?.data?.groups ?? []
  const save = async () => {
    await update.mutateAsync({
      key: 'MediaKeyGroups',
      value: JSON.stringify(value),
    })
    setDraft(null)
    await client.invalidateQueries({ queryKey: ['media-key-config'] })
  }

  if (config.isPending || groups.isPending) return <LoadingState />
  if (config.isError || groups.isError) {
    return (
      <ErrorState
        onRetry={() => {
          void config.refetch()
          void groups.refetch()
        }}
      />
    )
  }

  return (
    <SettingsSection title={t('Image and video keys')}>
      <SettingsPageFormActions
        onSave={() => {
          void save().catch(() => undefined)
        }}
        isSaving={update.isPending}
        isSaveDisabled={draft === null}
      />
      <fieldset disabled={update.isPending} className='max-w-2xl space-y-3'>
        <div className='text-sm font-medium'>{t('Available groups')}</div>
        {/* 复用分组排序交互，专用授权不提供恢复普通 Auto 配置的入口。 */}
        <AutoGroupOrderEditor
          value={value}
          mode='custom'
          allowInherit={false}
          options={available.map((group) => ({ value: group, label: group }))}
          globalOptions={[]}
          maxCount={Math.max(available.length, value.length, 1)}
          onChange={(next) => setDraft(next.groups)}
        />
      </fieldset>
    </SettingsSection>
  )
}
