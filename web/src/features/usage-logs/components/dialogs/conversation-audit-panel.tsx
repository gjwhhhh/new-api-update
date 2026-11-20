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
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { ApiResponse } from '@/features/profile/types'
import { api } from '@/lib/api'
import { getServerErrorStatus } from '@/lib/server-error-message'

import { DetailRow, DetailSection } from './log-detail-layout'

interface ConversationAuditDetail {
  audit: {
    status: string
    capture_error?: string
    error_code?: string
    full_payload: boolean
    request_truncated: boolean
    response_truncated: boolean
  }
  request: string
  response: string
  response_segments: string[]
  matched_segment?: { response_index: number; truncated: boolean }
}

function capturedText(payload: string, field: 'user_input' | 'assistant_text') {
  if (!payload) return ''
  try {
    const parsed = JSON.parse(payload)
    if (parsed?.schema_version === 2) {
      const text: unknown = parsed[field]
      return typeof text === 'string' ? text : ''
    }
  } catch {
    // Older records may contain unstructured text.
  }
  return payload
}

export function ConversationAuditPanel({ requestId }: { requestId: string }) {
  const { t } = useTranslation()
  const [result, setResult] = useState<
    | { detail: ConversationAuditDetail }
    | { error: 'missing' | 'forbidden' | 'unavailable' }
    | null
  >(null)

  const encodedId = encodeURIComponent(requestId)
  useEffect(() => {
    const controller = new AbortController()
    // Keep decrypted text local to the open panel, not in the shared query cache.
    api
      .get<ApiResponse<ConversationAuditDetail>>(
        `/api/custom/conversation-audits/${encodedId}`,
        { signal: controller.signal, skipErrorHandler: true }
      )
      .then(({ data }) => {
        if (controller.signal.aborted) return
        setResult(
          data.success && data.data
            ? { detail: data.data }
            : { error: 'unavailable' }
        )
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return
        const status = getServerErrorStatus(error)
        if (status === 404) {
          setResult({ error: 'missing' })
        } else if (status === 401 || status === 403) {
          setResult({ error: 'forbidden' })
        } else {
          setResult({ error: 'unavailable' })
        }
      })
    return () => controller.abort()
  }, [encodedId])

  if (!result) return <p role='status'>{t('Loading...')}</p>
  if ('error' in result) {
    const messages = {
      missing: t('No conversation content available'),
      forbidden: t('Access denied'),
      unavailable: t('Could not load conversation content'),
    }
    return <p role='status'>{messages[result.error]}</p>
  }

  const { audit, request, response, response_segments, matched_segment } =
    result.detail
  const input = audit.full_payload
    ? request
    : capturedText(request, 'user_input')
  const rootReply = audit.full_payload
    ? response
    : capturedText(response, 'assistant_text')
  const segments = response_segments.map((part) =>
    capturedText(part, 'assistant_text')
  )
  const reply = matched_segment
    ? (segments[matched_segment.response_index] ?? '')
    : rootReply
  const combined = [rootReply, ...segments].filter(Boolean).join('\n\n')
  const pending = audit.capture_error
    ?.split(',')
    .includes('pending_response_continuation')
  const statusLabel =
    {
      completed: t('Completed'),
      failed: t('Failed'),
      incomplete: t('Incomplete'),
      truncated: t('Truncated'),
      capture_failed: t('Capture failed'),
    }[audit.status] ?? audit.status
  const textClass = 'whitespace-pre-wrap wrap-anywhere text-sm'

  return (
    <div className='min-w-0 space-y-3 py-1'>
      <div className='space-y-1'>
        <DetailRow
          label={matched_segment ? t('Turn status') : t('Status')}
          value={statusLabel}
        />
        {audit.error_code && (
          <DetailRow label={t('Error code')} value={audit.error_code} mono />
        )}
        {audit.full_payload && (
          <p className='text-muted-foreground text-xs'>{t('Raw payload')}</p>
        )}
        {pending && (
          <p className='text-muted-foreground text-xs'>
            {t('Continuation pending association')}
          </p>
        )}
        {audit.request_truncated && (
          <p className='text-muted-foreground text-xs'>
            {t('Request truncated')}
          </p>
        )}
        {(matched_segment?.truncated ?? audit.response_truncated) && (
          <p className='text-muted-foreground text-xs'>
            {t('Response truncated')}
          </p>
        )}
      </div>

      <DetailSection label={t('Request')}>
        <p className={textClass}>{input || t('No captured text')}</p>
      </DetailSection>
      <DetailSection label={t('Response')}>
        <p className={textClass}>{reply || t('No captured text')}</p>
      </DetailSection>
      {segments.length > 0 && combined !== reply && (
        <details className='space-y-2'>
          <summary className='w-fit cursor-pointer text-sm font-medium'>
            {t('Combined turn response')}
          </summary>
          <p className={textClass}>{combined}</p>
        </details>
      )}
      <a
        className='text-primary text-sm underline-offset-4 hover:underline'
        href={`/conversation-audit/?request_id=${encodedId}`}
      >
        {t('View conversation audit')}
      </a>
    </div>
  )
}
