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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { RichContent } from '@/components/rich-content'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber, formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import { previewMail, submitMail, testMail } from '../api'
import { mailComposeSchema, type MailComposeValues } from '../lib/schema'
import type {
  MailConfiguration,
  MailPreview,
  RecipientSelection,
} from '../types'
import { MailRecipients } from './mail-recipients'

export function MailCompose(props: { configuration: MailConfiguration }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const client = useQueryClient()
  const form = useForm<MailComposeValues>({
    resolver: zodResolver(mailComposeSchema),
    defaultValues: { subject: '', body: '' },
  })
  const [selection, setSelection] = useState<RecipientSelection>({
    keyword: '',
    group: '',
    status: 1,
    ids: [],
    all: false,
  })
  const [prepared, setPrepared] = useState<{
    data: MailPreview
    fingerprint: string
  } | null>(null)
  const [confirm, setConfirm] = useState(false)
  const [testUser, setTestUser] = useState('')
  const bodyRef = useRef<HTMLTextAreaElement | null>(null)
  const values = form.watch()
  const fingerprint = JSON.stringify({
    ...values,
    subject: values.subject.trim(),
    selection,
  })
  const preview = prepared?.fingerprint === fingerprint ? prepared.data : null
  const enabled =
    props.configuration.settings.sending_enabled &&
    props.configuration.smtp_configured
  const previewMutation = useMutation({
    mutationFn: async (input: MailComposeValues) => {
      const snapshot = { ...input, selection }
      return {
        data: await previewMail(snapshot),
        fingerprint: JSON.stringify(snapshot),
      }
    },
    onSuccess: (result) => {
      setPrepared(result)
    },
    onError: (error) => handleServerError(error),
  })
  const submitMutation = useMutation({
    mutationFn: () => {
      if (!preview) throw new Error(t('Invalid email request'))
      return submitMail(preview.batch.id)
    },
    onSuccess: async () => {
      setConfirm(false)
      setPrepared(null)
      toast.success(t('Email batch queued'))
      await client.invalidateQueries({ queryKey: ['mail'] })
    },
    onError: (error) => handleServerError(error),
  })
  const testMutation = useMutation({
    mutationFn: () => {
      if (!preview) throw new Error(t('Invalid email request'))
      return testMail(preview.batch.id, Number(testUser) || userId)
    },
    onSuccess: async () => {
      toast.success(t('SMTP accepted the test email'))
      await client.invalidateQueries({ queryKey: ['mail', 'records'] })
    },
    onError: (error) => handleServerError(error),
  })
  function insertFormat(before: string, after: string) {
    const element = bodyRef.current
    if (!element) return
    const body = form.getValues('body')
    const start = element.selectionStart
    const end = element.selectionEnd
    form.setValue(
      'body',
      body.slice(0, start) +
        before +
        body.slice(start, end) +
        after +
        body.slice(end),
      { shouldDirty: true }
    )
    element.focus()
    requestAnimationFrame(() =>
      element.setSelectionRange(start + before.length, end + before.length)
    )
  }
  return (
    <div className='min-w-0 space-y-5 rounded-lg border p-4'>
      {!enabled ? (
        <Alert>
          <AlertDescription>
            {t(
              'Configure SMTP and enable email sending before creating a batch.'
            )}
          </AlertDescription>
        </Alert>
      ) : null}
      <MailRecipients
        selection={selection}
        onChange={setSelection}
        limit={props.configuration.settings.batch_limit}
      />
      <Form {...form}>
        <form
          className='space-y-4'
          onSubmit={form.handleSubmit((input) => previewMutation.mutate(input))}
        >
          <FormField
            control={form.control}
            name='subject'
            render={({ field, fieldState }) => (
              <FormItem>
                <FormLabel>{t('Subject')}</FormLabel>
                <FormControl>
                  <Input {...field} maxLength={200} />
                </FormControl>
                {fieldState.error ? (
                  <p role='alert' className='text-destructive text-sm'>
                    {t(fieldState.error.message ?? 'Invalid email request')}
                  </p>
                ) : null}
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='body'
            render={({ field, fieldState }) => (
              <FormItem>
                <FormLabel>{t('Email body')}</FormLabel>
                <div
                  className='flex flex-wrap gap-1'
                  role='toolbar'
                  aria-label={t('Email formatting')}
                >
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => insertFormat('**', '**')}
                  >
                    {t('Bold')}
                  </Button>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => insertFormat('\n- ', '')}
                  >
                    {t('List')}
                  </Button>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => insertFormat('[', '](https://example.com)')}
                  >
                    {t('Link')}
                  </Button>
                </div>
                <FormControl>
                  <Textarea
                    {...field}
                    ref={(element) => {
                      field.ref(element)
                      bodyRef.current = element
                    }}
                    className='min-h-56'
                  />
                </FormControl>
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'Supports paragraphs, bold text, lists and HTTP links. Images and attachments are not supported.'
                  )}
                </p>
                {fieldState.error ? (
                  <p role='alert' className='text-destructive text-sm'>
                    {t(fieldState.error.message ?? 'Invalid email request')}
                  </p>
                ) : null}
              </FormItem>
            )}
          />
          <Button
            type='submit'
            disabled={
              !enabled ||
              previewMutation.isPending ||
              (!selection.all && selection.ids.length === 0)
            }
          >
            {previewMutation.isPending
              ? t('Loading...')
              : t('Preview recipients and email')}
          </Button>
        </form>
      </Form>
      {preview ? (
        <section
          className='space-y-4 border-t pt-4'
          aria-label={t('Email preview')}
        >
          <h3 className='font-medium'>{preview.message.subject}</h3>
          <p role='status'>
            {t(
              'Send to {{recipients}} mailboxes · {{excluded}} excluded · {{duplicates}} duplicates removed',
              {
                recipients: formatNumber(preview.batch.total, locale),
                excluded: formatNumber(preview.batch.exclusions.length, locale),
                duplicates: formatNumber(preview.batch.duplicates, locale),
              }
            )}
          </p>
          <p className='text-muted-foreground text-xs'>
            {t('Confirmation expires at {{time}}', {
              time: formatTimestampToDate(preview.batch.expires_at),
            })}
          </p>
          <RichContent
            mode='html'
            content={preview.message.body}
            className='rounded-md border p-4 break-words'
          />
          <div className='text-muted-foreground space-y-1 text-xs'>
            <p>{t('Recipient sample')}</p>
            {preview.recipients.map((recipient) => (
              <p key={recipient.email} className='break-all'>
                {recipient.email}
              </p>
            ))}
          </div>
          {preview.batch.exclusions.length > 0 ? (
            <div className='space-y-1 text-sm'>
              {preview.batch.exclusions.map((excluded) => (
                <p key={excluded.user_id}>
                  {excluded.user_id} · {excluded.email || '-'} ·{' '}
                  {t(excluded.reason)}
                </p>
              ))}
            </div>
          ) : null}
          <div className='flex flex-wrap gap-2'>
            <Input
              aria-label={t('Test recipient user ID')}
              placeholder={t('Test recipient user ID (defaults to you)')}
              type='number'
              min={1}
              className='w-full sm:w-72'
              value={testUser}
              onChange={(event) => setTestUser(event.target.value)}
            />
            <Button
              variant='outline'
              disabled={testMutation.isPending}
              onClick={() => testMutation.mutate()}
            >
              {t('Send test email')}
            </Button>
            <Button
              disabled={submitMutation.isPending || !enabled}
              onClick={() => setConfirm(true)}
            >
              {t('Confirm and send')}
            </Button>
          </div>
        </section>
      ) : null}
      <ConfirmDialog
        open={confirm && preview !== null}
        onOpenChange={setConfirm}
        title={t('Send email batch?')}
        desc={t(
          'This sends one email to each selected mailbox. Sent email cannot be recalled.'
        )}
        confirmText={t('Send email')}
        isLoading={submitMutation.isPending}
        handleConfirm={() => submitMutation.mutate()}
      />
    </div>
  )
}
