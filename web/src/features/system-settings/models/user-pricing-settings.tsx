import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  useModelPricing,
  type ModelPricingChange,
} from '@/features/model-pricing/api'
import { pricingFromDraft, pricingRow } from '@/features/model-pricing/pricing'
import {
  getUserModelPricing,
  saveUserModelPricing,
} from '@/features/model-pricing/user-pricing-api'
import type { User } from '@/features/users/types'
import { handleServerError } from '@/lib/handle-server-error'

import {
  ModelPricingEditorPanel,
  type ModelPricingEditorPanelHandle,
} from './model-pricing-sheet'
import { UserPricingSelector } from './user-pricing-selector'

export function UserPricingSettings() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [user, setUser] = useState<User | null>(null)
  const [modelName, setModelName] = useState('')
  const [dirty, setDirty] = useState(false)
  const [pendingSelection, setPendingSelection] = useState<{
    user: User | null
    model: string
  } | null>(null)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const editor = useRef<ModelPricingEditorPanelHandle>(null)
  const globalQuery = useModelPricing()
  const query = useQuery({
    queryKey: ['user-model-pricing', user?.id],
    queryFn: () => getUserModelPricing(user?.id ?? 0),
    enabled: user !== null,
    refetchOnWindowFocus: false,
  })
  const mutation = useMutation({
    mutationFn: (input: { userId: number; change: ModelPricingChange }) =>
      saveUserModelPricing(input.userId, [input.change]),
    onSuccess: async (_, input) => {
      setDirty(false)
      setDeleteOpen(false)
      await client.invalidateQueries({
        queryKey: ['user-model-pricing', input.userId],
      })
      await client.invalidateQueries({ queryKey: ['pricing'] })
      toast.success(t('Saved successfully'))
    },
  })
  const configured = query.data?.entries.find(
    (entry) => entry.model_name === modelName
  )
  const inherited = globalQuery.data?.entries.find(
    (entry) => entry.model_name === modelName
  )
  const metadata = configured ?? inherited
  // 编辑器只生成当前用户的草稿，保存入口始终使用用户定价接口。
  const draft = useMemo(
    () =>
      modelName
        ? pricingRow(
            modelName,
            metadata?.effective ?? {
              'billing_setting.billing_mode': 'tiered_expr',
              'billing_setting.billing_expr': 'tier("base", p * 0 + c * 0)',
            }
          )
        : undefined,
    [modelName, metadata]
  )
  const modelOptions = useMemo(
    () =>
      [
        ...new Set([
          ...(globalQuery.data?.entries.map((entry) => entry.model_name) ?? []),
          ...(query.data?.entries.map((entry) => entry.model_name) ?? []),
        ]),
      ]
        .sort()
        .map((name) => ({ value: name, label: name })),
    [globalQuery.data, query.data]
  )

  const select = (next: { user: User | null; model: string }) => {
    if (dirty) {
      setPendingSelection(next)
      return
    }
    setUser(next.user)
    setModelName(next.model)
  }
  const save = async () => {
    try {
      const data = await editor.current?.commitDraft()
      if (!data || !user || !query.data) return
      await mutation.mutateAsync({
        userId: user.id,
        change: {
          model_name: modelName,
          expected_version: configured?.version ?? query.data.empty_version,
          pricing: pricingFromDraft(data),
        },
      })
    } catch (error) {
      handleServerError(error)
    }
  }

  return (
    <div className='flex min-h-0 flex-col gap-4'>
      <p className='text-muted-foreground text-sm'>
        {t(
          'User prices replace model base prices. Group ratios still apply. Models without overrides use global prices.'
        )}
      </p>
      <div className='grid min-h-0 gap-4 lg:grid-cols-[20rem_minmax(0,1fr)]'>
        <div className='flex min-w-0 flex-col gap-4'>
          <UserPricingSelector
            user={user}
            disabled={mutation.isPending}
            onSelect={(next) => select({ user: next, model: '' })}
          />
          {user && (
            <>
              {query.isPending && <LoadingState />}
              {query.isError && (
                <ErrorState
                  description={query.error.message}
                  onRetry={() => void query.refetch()}
                />
              )}
              {query.isSuccess && (
                <>
                  <Combobox
                    aria-label={t('Select model')}
                    placeholder={t('Select model')}
                    options={modelOptions}
                    value={modelName}
                    disabled={mutation.isPending}
                    onValueChange={(name) =>
                      select({ user, model: name ?? '' })
                    }
                  />
                  <ScrollArea className='h-56 rounded-md border'>
                    <div className='flex flex-col gap-1 p-2'>
                      {query.data?.entries.map((entry) => (
                        <Button
                          key={entry.model_name}
                          variant={
                            modelName === entry.model_name
                              ? 'secondary'
                              : 'ghost'
                          }
                          className='h-auto justify-start text-left break-all whitespace-normal'
                          aria-pressed={modelName === entry.model_name}
                          disabled={mutation.isPending}
                          onClick={() =>
                            select({ user, model: entry.model_name })
                          }
                        >
                          {entry.model_name}
                        </Button>
                      ))}
                      {!query.data?.entries.length && (
                        <p className='text-muted-foreground p-3 text-sm'>
                          {t('No user prices configured')}
                        </p>
                      )}
                    </div>
                  </ScrollArea>
                </>
              )}
            </>
          )}
        </div>
        <div className='min-w-0'>
          {globalQuery.isError && (
            <ErrorState
              description={globalQuery.error.message}
              onRetry={() => void globalQuery.refetch()}
            />
          )}
          {!globalQuery.isError &&
            user &&
            modelName &&
            query.isSuccess &&
            draft && (
              <div className='flex min-w-0 flex-col gap-3'>
                <div className='flex flex-wrap items-center justify-between gap-2'>
                  <span className='text-sm'>
                    {configured
                      ? t('User price configured')
                      : t('Using global price')}
                  </span>
                  {configured && (
                    <Button
                      size='sm'
                      variant='outline'
                      disabled={mutation.isPending}
                      onClick={() => setDeleteOpen(true)}
                    >
                      {t('Restore global price')}
                    </Button>
                  )}
                </div>
                {mutation.isError && (
                  <ErrorState
                    description={mutation.error.message}
                    onRetry={() => {
                      mutation.reset()
                      void query.refetch()
                    }}
                  />
                )}
                <ModelPricingEditorPanel
                  key={`${user.id}:${modelName}:${configured?.version ?? 'new'}`}
                  ref={editor}
                  userPricing
                  editData={draft}
                  usageSchema={metadata?.usage_schema}
                  pluginVariants={metadata?.plugin_variants}
                  onSave={save}
                  isSaving={mutation.isPending}
                  onDirtyChange={setDirty}
                  className='h-[42rem] max-h-[80dvh]'
                />
              </div>
            )}
          {(!user || !modelName) && (
            <p className='text-muted-foreground p-6 text-sm'>
              {t('Select a user and model to configure pricing')}
            </p>
          )}
        </div>
      </div>
      <ConfirmDialog
        open={pendingSelection !== null}
        onOpenChange={(open) => {
          if (!open) setPendingSelection(null)
        }}
        title={t('Discard unsaved changes?')}
        desc={t('Your unsaved pricing changes will be discarded.')}
        confirmText={t('Discard')}
        handleConfirm={() => {
          if (!pendingSelection) return
          setUser(pendingSelection.user)
          setModelName(pendingSelection.model)
          setDirty(false)
          setPendingSelection(null)
        }}
      />
      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={t('Restore global price')}
        desc={t(
          'Remove this user price? Future requests will use the global model price and group ratio.'
        )}
        destructive
        isLoading={mutation.isPending}
        confirmText={t('Restore')}
        handleConfirm={() => {
          if (!user || !configured) return
          mutation.mutate({
            userId: user.id,
            change: {
              model_name: modelName,
              expected_version: configured.version,
              pricing: {},
              reset: true,
            },
          })
        }}
      />
    </div>
  )
}
