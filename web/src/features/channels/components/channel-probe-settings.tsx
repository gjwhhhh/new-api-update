import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Eye, Play, Save, WandSparkles } from 'lucide-react'
import { useEffect, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { channelProbeUsageRows } from '@/features/channel-test-history/probe-usage'
import { formatNumber } from '@/lib/format'

import {
  getChannelProbe,
  previewChannelProbe,
  saveChannelProbe,
  testChannelProbe,
  type ChannelProbeCacheAffinity,
  type ChannelProbeConfig,
  type ChannelProbePreview,
} from '../api'

type Props = {
  channelId: number
  disabled: boolean
  canRun: boolean
}

export function ChannelProbeSettings(props: Props) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['channel-probe', props.channelId],
    queryFn: () => getChannelProbe(props.channelId),
    staleTime: 0,
  })
  if (query.isError) {
    return (
      <Button
        type='button'
        variant='outline'
        onClick={() => void query.refetch()}
      >
        {t('Retry loading probe settings')}
      </Button>
    )
  }
  // Wait for this mount's fetch so a stale cache hit cannot pin the form.
  if (!query.isFetchedAfterMount || !query.data) {
    return <p className='text-muted-foreground text-sm'>{t('Loading')}</p>
  }
  if (!query.data.supported && query.data.config.mode === 'short') return null

  return (
    <ProbeForm
      {...props}
      key={props.channelId}
      supported={query.data.supported}
      cacheAffinity={query.data.cache_affinity}
      initialConfig={query.data.config}
      configRevision={query.dataUpdatedAt}
    />
  )
}

function ProbeForm(
  props: Props & {
    supported: boolean
    cacheAffinity: ChannelProbeCacheAffinity
    initialConfig: ChannelProbeConfig
    configRevision: number
  }
) {
  const { t } = useTranslation()
  const id = useId()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState(props.initialConfig)
  const [saved, setSaved] = useState(props.initialConfig)
  const [target, setTarget] = useState(String(draft.target_tokens ?? 24_000))
  const [preview, setPreview] = useState<ChannelProbePreview>()
  const [confirmTrial, setConfirmTrial] = useState(false)
  const [error, setError] = useState('')
  const [appliedRevision, setAppliedRevision] = useState(props.configRevision)

  const targetNumber = Number(target)
  const validTarget =
    Number.isInteger(targetNumber) &&
    targetNumber >= 1024 &&
    targetNumber <= 48_000
  const dirty =
    draft !== saved ||
    (draft.mode === 'auto' && targetNumber !== draft.target_tokens)
  const canSave =
    draft.mode === 'short' ||
    (props.supported &&
      (draft.mode === 'auto'
        ? Boolean(draft.seed) && targetNumber === draft.target_tokens
        : Boolean(
            draft.context?.trim() &&
            draft.question?.trim() &&
            draft.expected_answer?.trim()
          )))

  useEffect(() => {
    if (props.configRevision === appliedRevision) return
    if (dirty) return
    setDraft(props.initialConfig)
    setSaved(props.initialConfig)
    setTarget(String(props.initialConfig.target_tokens ?? 24_000))
    setPreview(undefined)
    setError('')
    setAppliedRevision(props.configRevision)
  }, [
    appliedRevision,
    dirty,
    props.configRevision,
    props.initialConfig,
  ])

  const save = useMutation({
    mutationFn: () =>
      saveChannelProbe(props.channelId, {
        ...draft,
        cache_affinity:
          draft.mode !== 'short' && draft.cache_affinity === props.cacheAffinity
            ? draft.cache_affinity
            : 'none',
      }),
    onSuccess: (config) => {
      setSaved(config)
      setDraft(config)
      setTarget(String(config.target_tokens ?? 24_000))
      setAppliedRevision(props.configRevision)
      queryClient.setQueryData(['channel-probe', props.channelId], {
        supported: props.supported,
        cache_affinity: props.cacheAffinity,
        config,
      })
    },
    onError: () => setError(t('Could not save probe settings')),
  })
  const generate = useMutation({
    mutationFn: (tokens?: number) =>
      previewChannelProbe(props.channelId, tokens),
    onSuccess: (data, tokens) => {
      setPreview(data)
      if (tokens !== undefined) {
        setDraft((current) => ({
          ...data.config,
          cache_affinity: current.cache_affinity,
        }))
      }
    },
    onError: () => setError(t('Could not preview probe content')),
  })
  const trial = useMutation({
    mutationFn: () => testChannelProbe(props.channelId),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: ['channel-test-history'] })
    },
    onError: () => setError(t('Could not run the saved probe')),
  })
  const busy =
    props.disabled || save.isPending || generate.isPending || trial.isPending
  const modes = [
    { value: 'short', label: t('Default short probe') },
    { value: 'auto', label: t('Generated long context') },
    { value: 'custom', label: t('Custom long context') },
  ] as const

  return (
    <section className='space-y-3 border-t pt-4'>
      <fieldset className='space-y-3 disabled:opacity-60' disabled={busy}>
        <div className='space-y-2'>
          <Label htmlFor={`${id}-mode`}>{t('Probe content')}</Label>
          <Select
            items={modes}
            value={draft.mode}
            disabled={busy}
            onValueChange={(mode) => {
              if (!mode || mode === draft.mode) return
              setError('')
              setPreview(undefined)
              setDraft(
                mode === saved.mode
                  ? saved
                  : { mode, cache_affinity: draft.cache_affinity }
              )
              setTarget(String(saved.target_tokens ?? 24_000))
            }}
          >
            <SelectTrigger id={`${id}-mode`}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              {modes.map(({ value, label }) => (
                <SelectItem
                  key={value}
                  value={value}
                  disabled={!props.supported && value !== 'short'}
                >
                  {label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {!props.supported && (
            <p className='text-muted-foreground text-sm'>
              {t('This channel does not support long probes.')}
            </p>
          )}
        </div>

        {props.supported && draft.mode === 'auto' && (
          <div className='space-y-2'>
            <Label htmlFor={`${id}-target`}>{t('Target input tokens')}</Label>
            <Input
              id={`${id}-target`}
              type='number'
              min={1024}
              max={48_000}
              step={1}
              value={target}
              onChange={(event) => {
                setTarget(event.target.value)
                setPreview(undefined)
              }}
            />
            <div className='flex flex-wrap gap-2'>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={!validTarget}
                onClick={() => {
                  setError('')
                  generate.mutate(targetNumber)
                }}
              >
                <WandSparkles aria-hidden='true' />
                {draft.seed ? t('Regenerate probe') : t('Generate probe')}
              </Button>
              {saved.mode === 'auto' && !dirty && (
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={() => {
                    setError('')
                    generate.mutate(undefined)
                  }}
                >
                  <Eye aria-hidden='true' />
                  {t('Preview')}
                </Button>
              )}
            </div>
            {preview && (
              <details className='space-y-2 rounded-md border p-2'>
                <summary className='cursor-pointer text-sm'>
                  {t('Estimated input: {{count}} tokens', {
                    count: formatNumber(preview.estimated_input_tokens),
                  })}
                </summary>
                <pre className='max-h-40 overflow-auto text-xs break-words whitespace-pre-wrap'>
                  {preview.context}
                </pre>
                <p className='text-sm'>
                  {t('Question')}: {preview.question}
                </p>
                <p className='text-sm'>
                  {t('Expected answer')}: {preview.expected_answer}
                </p>
              </details>
            )}
          </div>
        )}

        {props.supported && draft.mode === 'custom' && (
          <>
            <div className='space-y-2'>
              <Label htmlFor={`${id}-context`}>{t('Context')}</Label>
              <Textarea
                id={`${id}-context`}
                rows={5}
                maxLength={393_216}
                value={draft.context ?? ''}
                onChange={(event) =>
                  setDraft({ ...draft, context: event.target.value })
                }
              />
            </div>
            <div className='space-y-2'>
              <Label htmlFor={`${id}-question`}>{t('Question')}</Label>
              <Input
                id={`${id}-question`}
                maxLength={2048}
                value={draft.question ?? ''}
                onChange={(event) =>
                  setDraft({ ...draft, question: event.target.value })
                }
              />
            </div>
            <div className='space-y-2'>
              <Label htmlFor={`${id}-answer`}>{t('Expected answer')}</Label>
              <Input
                id={`${id}-answer`}
                maxLength={128}
                value={draft.expected_answer ?? ''}
                onChange={(event) =>
                  setDraft({ ...draft, expected_answer: event.target.value })
                }
              />
            </div>
          </>
        )}

        {draft.mode !== 'short' && props.cacheAffinity !== 'none' && (
          <div className='flex items-center gap-2'>
            <Checkbox
              id={`${id}-affinity`}
              checked={draft.cache_affinity === props.cacheAffinity}
              onCheckedChange={(value) =>
                setDraft({
                  ...draft,
                  cache_affinity: value ? props.cacheAffinity : 'none',
                })
              }
            />
            <Label htmlFor={`${id}-affinity`}>
              {props.cacheAffinity === 'grok_header'
                ? t('Use a fixed Grok conversation ID')
                : t('Use a fixed Claude prompt cache')}
            </Label>
          </div>
        )}

        <div className='flex flex-wrap gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={!dirty || !canSave}
            onClick={() => {
              setError('')
              trial.reset()
              save.mutate()
            }}
          >
            <Save aria-hidden='true' />
            {t('Save probe settings')}
          </Button>
          {props.supported && props.canRun && saved.mode !== 'short' && (
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={dirty}
              onClick={() => setConfirmTrial(true)}
            >
              <Play aria-hidden='true' />
              {t('Run saved probe')}
            </Button>
          )}
        </div>
      </fieldset>

      {error && (
        <p className='text-destructive text-sm' role='alert'>
          {error}
        </p>
      )}
      {trial.isPending && (
        <p className='text-muted-foreground text-sm' role='status'>
          {t('Running long probe')}
        </p>
      )}
      {trial.data && (
        <div className='space-y-1 text-sm' role='status'>
          <p className='font-medium'>
            {t(trial.data.status)}
            {trial.data.status !== 'succeeded' &&
              ` · ${t(trial.data.failure_kind)}`}
          </p>
          <dl className='grid grid-cols-[minmax(0,1fr)_auto] gap-x-3 gap-y-1'>
            {channelProbeUsageRows(trial.data, t).map(([label, value]) => (
              <div key={label} className='contents'>
                <dt className='text-muted-foreground'>{label}</dt>
                <dd>{value}</dd>
              </div>
            ))}
          </dl>
        </div>
      )}
      <ConfirmDialog
        open={confirmTrial}
        onOpenChange={setConfirmTrial}
        title={t('Run saved probe')}
        desc={t(
          'This sends one request to the upstream provider and may incur charges.'
        )}
        confirmText={t('Run saved probe')}
        handleConfirm={() => {
          setConfirmTrial(false)
          setError('')
          trial.mutate()
        }}
      />
    </section>
  )
}
