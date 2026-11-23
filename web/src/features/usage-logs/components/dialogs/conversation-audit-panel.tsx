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

import { Button } from '@/components/ui/button'
import type { ApiResponse } from '@/features/profile/types'
import { api } from '@/lib/api'
import { getServerErrorStatus } from '@/lib/server-error-message'

import { DetailRow, DetailSection } from './log-detail-layout'

interface ConversationAuditDetail {
  preview?: boolean
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
  request_preview?: string
  response_preview?: string
  request_preview_truncated?: boolean
  response_preview_truncated?: boolean
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
  const [full, setFull] = useState(false)
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
        `/api/custom/conversation-audits/${encodedId}?preview=${full ? 'false' : 'true'}`,
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
  }, [encodedId, full])

  if (!result) return <p role='status'>{t('Loading...')}</p>
  if ('error' in result) {
    const messages = {
      missing: t('Conversation audit record not found'),
      forbidden: t('Access denied'),
      unavailable: t('Could not load conversation content'),
    }
    return <p role='status'>{messages[result.error]}</p>
  }

  const { audit } = result.detail
  const isPreview = result.detail.preview === true
  let input = result.detail.request_preview ?? ''
  if (!isPreview) {
    input = audit.full_payload
      ? result.detail.request
      : capturedText(result.detail.request, 'user_input')
  }
  let rootReply = result.detail.response_preview ?? ''
  if (!isPreview) {
    rootReply = audit.full_payload
      ? result.detail.response
      : capturedText(result.detail.response, 'assistant_text')
  }
  const segments = isPreview
    ? []
    : result.detail.response_segments.map((part) =>
        capturedText(part, 'assistant_text')
      )
  const reply = result.detail.matched_segment
    ? (segments[result.detail.matched_segment.response_index] ?? '')
    : rootReply
  const combined = [rootReply, ...segments].filter(Boolean).join('\n\n')
  const captureReasons = audit.capture_error?.split(',') ?? []
  const pending = captureReasons.includes('pending_response_continuation')
  const requestOverLimit = captureReasons.includes(
    'request_parse_limit_exceeded'
  )
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
          label={result.detail.matched_segment ? t('Turn status') : t('Status')}
          value={statusLabel}
        />
        {audit.error_code && (
          <DetailRow label={t('Error code')} value={audit.error_code} mono />
        )}
        {!isPreview && audit.full_payload && (
          <p className='text-muted-foreground text-xs'>{t('Raw payload')}</p>
        )}
        {pending && (
          <p className='text-muted-foreground text-xs'>
            {t('Continuation pending association')}
          </p>
        )}
        {(audit.request_truncated ||
          result.detail.request_preview_truncated) && (
          <p className='text-muted-foreground text-xs'>
            {t('Request truncated')}
          </p>
        )}
        {((result.detail.matched_segment?.truncated ??
          audit.response_truncated) ||
          result.detail.response_preview_truncated) && (
          <p className='text-muted-foreground text-xs'>
            {t('Response truncated')}
          </p>
        )}
      </div>

      <DetailSection label={t('Request')}>
        <p className={textClass}>
          {input ||
            (requestOverLimit
              ? t('Request exceeded the audit size limit and was not captured')
              : t('No captured text'))}
        </p>
      </DetailSection>
      <DetailSection label={t('Response')}>
        <p className={textClass}>{reply || t('No captured text')}</p>
      </DetailSection>
      {isPreview ? (
        <Button
          type='button'
          variant='link'
          size='sm'
          className='h-auto p-0'
          onClick={() => {
            setResult(null)
            setFull(true)
          }}
        >
          {t('Expand')}
        </Button>
      ) : (
        segments.length > 0 &&
        combined !== reply && (
          <details className='space-y-2'>
            <summary className='w-fit cursor-pointer text-sm font-medium'>
              {t('Combined turn response')}
            </summary>
            <p className={textClass}>{combined}</p>
          </details>
        )
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
