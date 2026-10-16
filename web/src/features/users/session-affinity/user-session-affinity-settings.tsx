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
import { useQuery } from '@tanstack/react-query'
import {
  getCoreRowModel,
  useReactTable,
  type PaginationState,
} from '@tanstack/react-table'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DataTablePagination,
  StaticDataTable,
  TruncatedCell,
} from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { SettingsCard } from '@/features/system-settings/components/settings-card'
import { useDebounce } from '@/hooks/use-debounce'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

import {
  searchUserSessionAffinities,
  userSessionAffinityQueryKey,
  type UserSessionAffinity,
} from './api'
import { UserSessionAffinityDialog } from './user-session-affinity-dialog'

export function UserSessionAffinitySettings(props: {
  globalEnabled: boolean
  hasGlobalChanges: boolean
}) {
  const currentUser = useAuthStore((state) => state.auth.user)
  if (
    !hasPermission(
      currentUser,
      ADMIN_PERMISSION_RESOURCES.CHANNEL,
      ADMIN_PERMISSION_ACTIONS.WRITE
    )
  ) {
    return null
  }
  return <UserSessionAffinityEditor {...props} />
}

function UserSessionAffinityEditor(props: {
  globalEnabled: boolean
  hasGlobalChanges: boolean
}) {
  const { t } = useTranslation()
  const [selecting, setSelecting] = useState(false)
  const [target, setTarget] = useState<{
    user: UserSessionAffinity
    adding: boolean
  } | null>(null)
  return (
    <SettingsCard
      title={t('User configuration')}
      description={t(
        'Users follow system policy unless listed here with session affinity disabled.'
      )}
    >
      <div className='space-y-4'>
        {!props.globalEnabled && (
          <Alert>
            <AlertDescription>
              {t(
                'Session affinity is currently disabled globally. User overrides are retained when it is enabled again.'
              )}
            </AlertDescription>
          </Alert>
        )}
        {props.hasGlobalChanges && (
          <p className='text-muted-foreground text-sm'>
            {t('Global changes take effect after saving settings.')}
          </p>
        )}
        <Button variant='outline' onClick={() => setSelecting(true)}>
          {t('Add user configuration')}
        </Button>
        <UserSessionAffinityList
          disabledOnly
          onSelect={(user) => setTarget({ user, adding: false })}
        />
      </div>
      {selecting && (
        <Dialog
          open
          onOpenChange={setSelecting}
          title={t('Add user configuration')}
          description={t('Search by user ID or username and select a user.')}
        >
          <UserSessionAffinityList
            disabledOnly={false}
            onSelect={(user) => {
              setSelecting(false)
              setTarget({ user, adding: !user.disabled })
            }}
          />
        </Dialog>
      )}
      {target && (
        <UserSessionAffinityDialog
          open
          onOpenChange={(open) => {
            if (!open) setTarget(null)
          }}
          user={{ id: target.user.user_id, username: target.user.username }}
          initialDisabled={target.adding}
        />
      )}
    </SettingsCard>
  )
}

function UserSessionAffinityList(props: {
  disabledOnly: boolean
  onSelect: (user: UserSessionAffinity) => void
}) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const keyword = useDebounce(search, 250)
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 10,
  })
  const query = useQuery({
    queryKey: [
      ...userSessionAffinityQueryKey,
      'list',
      props.disabledOnly,
      keyword,
      pagination,
    ],
    queryFn: ({ signal }) =>
      searchUserSessionAffinities(
        {
          keyword,
          disabled_only: props.disabledOnly,
          p: pagination.pageIndex + 1,
          page_size: pagination.pageSize,
        },
        signal
      ),
    refetchOnWindowFocus: 'always',
    meta: { errorToast: false },
  })
  const rows = query.data?.items ?? []
  const total = query.data?.total
  useEffect(() => {
    if (total === undefined) return
    const lastPage = Math.max(0, Math.ceil(total / pagination.pageSize) - 1)
    if (pagination.pageIndex > lastPage) {
      setPagination((previous) => ({ ...previous, pageIndex: lastPage }))
    }
  }, [total, pagination.pageIndex, pagination.pageSize])
  const table = useReactTable({
    data: rows,
    columns: [],
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    rowCount: query.data?.total ?? 0,
    state: { pagination },
    onPaginationChange: setPagination,
  })
  return (
    <div className='space-y-3'>
      <Input
        aria-label={t('Search by user ID or username')}
        placeholder={t('Search by user ID or username')}
        value={search}
        onChange={(event) => {
          setSearch(event.target.value)
          setPagination((previous) => ({ ...previous, pageIndex: 0 }))
        }}
      />
      {query.isPending && <LoadingState />}
      {query.isError && (
        <ErrorState
          title={t('Failed to load user configuration')}
          onRetry={() => void query.refetch()}
        />
      )}
      {query.isSuccess && (
        <>
          <StaticDataTable
            containerProps={{
              role: 'region',
              'aria-label': t('User session affinity configuration'),
            }}
            data={rows}
            getRowKey={(user) => user.user_id}
            columns={[
              {
                id: 'user',
                header: t('User'),
                cell: (user) => (
                  <div className='min-w-0'>
                    <TruncatedCell>{user.username}</TruncatedCell>
                    <p className='text-muted-foreground text-xs'>
                      ID {user.user_id}
                    </p>
                  </div>
                ),
              },
              {
                id: 'policy',
                header: t('Session affinity'),
                cell: (user) =>
                  user.disabled
                    ? t('Disable session affinity')
                    : t('Follow system policy'),
              },
              {
                id: 'actions',
                header: t('Actions'),
                cell: (user) => (
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={() => props.onSelect(user)}
                  >
                    {props.disabledOnly ? t('Edit') : t('Select')}
                  </Button>
                ),
              },
            ]}
            emptyContent={
              <EmptyState
                className='min-h-36'
                title={
                  keyword || !props.disabledOnly
                    ? t('No matching users')
                    : t('All users follow system policy')
                }
              />
            }
          />
          <DataTablePagination table={table} compact />
        </>
      )}
    </div>
  )
}
