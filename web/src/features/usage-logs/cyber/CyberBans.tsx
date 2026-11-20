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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { RefreshCw, Unlock } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { CopyButton } from '@/components/copy-button'
import { StaticDataTable } from '@/components/data-table'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { formatTimestamp } from '@/lib/format'
import {
  getServerErrorMessage,
  getServerErrorStatus,
} from '@/lib/server-error-message'

import { fetchCyber, revokeCyberBan, type CyberBan } from './api'

export function CyberBans(props: { userId: number; canWrite: boolean }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [cursor, setCursor] = useState('0')
  const [target, setTarget] = useState<CyberBan | null>(null)
  const [reason, setReason] = useState('')
  const query = useQuery({
    queryKey: ['cyber', props.userId, 'bans', cursor],
    queryFn: ({ signal }) => fetchCyber('bans', { cursor }, signal),
    retry: false,
    meta: { errorToast: false },
  })
  const mutation = useMutation({
    mutationFn: (ban: CyberBan) => revokeCyberBan(ban, reason.trim()),
    meta: { errorToast: false },
    onSuccess: () => {
      setTarget(null)
      toast.success(t('Session ban revoked'))
    },
    onSettled: () =>
      client.invalidateQueries({ queryKey: ['cyber', props.userId, 'bans'] }),
  })
  const data = query.data

  return (
    <div className='min-w-0 space-y-4 py-3'>
      <Button
        variant='outline'
        onClick={() => {
          if (cursor === '0') void query.refetch()
          else setCursor('0')
        }}
      >
        <RefreshCw aria-hidden />
        {t('Refresh')}
      </Button>
      {query.isPending && <LoadingState />}
      {query.isError && (
        <ErrorState
          description={
            getServerErrorStatus(query.error) === 503
              ? t('Cyber ban storage unavailable')
              : getServerErrorMessage(query.error)
          }
          onRetry={() => void query.refetch()}
        />
      )}
      {!query.isError && data && (
        <>
          {data.items.length === 0 ? (
            <EmptyState
              title={
                cursor === '0' && data.cursor === '0'
                  ? t('No active session bans')
                  : t('No bans in this scan window')
              }
            />
          ) : (
            <StaticDataTable
              data={data.items}
              getRowKey={(row) =>
                `${row.channel_id}:${row.token_id}:${row.fingerprint}`
              }
              columns={[
                {
                  id: 'session',
                  header: t('Session fingerprint'),
                  cell: (row) => (
                    <div className='flex items-center gap-1'>
                      <code>{row.fingerprint.slice(0, 12)}</code>
                      <CopyButton value={row.fingerprint} />
                    </div>
                  ),
                },
                {
                  id: 'token',
                  header: t('Token ID'),
                  cell: (row) => `#${row.token_id}`,
                },
                {
                  id: 'channel',
                  header: t('Channel ID'),
                  cell: (row) =>
                    row.channel_id ? `#${row.channel_id}` : t('All'),
                },
                {
                  id: 'created',
                  header: t('Created At'),
                  cell: (row) =>
                    row.created_at ? formatTimestamp(row.created_at) : '–',
                },
                {
                  id: 'expires',
                  header: t('Expiration'),
                  cell: (row) =>
                    row.permanent ? (
                      <Badge variant='secondary'>{t('Permanent')}</Badge>
                    ) : (
                      formatTimestamp(row.expires_at ?? 0)
                    ),
                },
                ...(!props.canWrite
                  ? []
                  : [
                      {
                        id: 'revoke',
                        header: t('Actions'),
                        cell: (row: CyberBan) => (
                          <Button
                            size='sm'
                            variant='outline'
                            onClick={() => {
                              mutation.reset()
                              setReason('')
                              setTarget(row)
                            }}
                          >
                            <Unlock aria-hidden />
                            {t('Revoke ban')}
                          </Button>
                        ),
                      },
                    ]),
              ]}
            />
          )}
          {data.cursor !== '0' && (
            <Button variant='outline' onClick={() => setCursor(data.cursor)}>
              {t('Next')}
            </Button>
          )}
        </>
      )}
      <ConfirmDialog
        open={props.canWrite && target !== null}
        onOpenChange={(open) => {
          if (!open && !mutation.isPending) setTarget(null)
        }}
        title={t('Revoke ban')}
        desc={
          target
            ? `#${target.token_id} · ${target.fingerprint.slice(0, 12)}`
            : ''
        }
        confirmText={t('Revoke ban')}
        disabled={
          !reason.trim() || new TextEncoder().encode(reason.trim()).length > 256
        }
        isLoading={mutation.isPending}
        handleConfirm={() => {
          if (target) mutation.mutate(target)
        }}
      >
        <label className='grid gap-1.5 text-sm'>
          {t('Reason')}
          <Input
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            maxLength={256}
          />
        </label>
        {mutation.isError && (
          <p role='alert' className='text-destructive text-sm'>
            {getServerErrorMessage(mutation.error)}
          </p>
        )}
      </ConfirmDialog>
    </div>
  )
}
