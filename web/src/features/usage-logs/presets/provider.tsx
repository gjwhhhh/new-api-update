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
import { getRouteApi, useBlocker } from '@tanstack/react-router'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import { useLogsViewScope } from '../components/usage-logs-provider'
import type { CommonLogFilters } from '../types'
import {
  createPreset,
  deletePreset,
  readPreset,
  readPresets,
  setDefaultPreset,
  updatePreset,
  type LogFilterPreset,
  type PresetCollection,
  type PresetScope,
  type PresetFilters,
} from './api'
import { LogPresetContext, type LogPresetContextValue } from './context'
import {
  draftFromPreset,
  draftFromSearch,
  draftSearch,
  hasExplicitLogFilters,
  logFilterSearchKey,
  samePresetFilters,
  savedFilters,
  type LogFilterDraft,
  type LogFilterSearch,
} from './filters'
import { PresetNameDialog } from './name-dialog'

const route = getRouteApi('/_authenticated/usage-logs/$section')
type EditorState = {
  ready: boolean
  initialized: boolean
  applied: LogFilterSearch
  draft: LogFilterDraft
  selected: string
  baseline: LogFilterPreset | null
  origin: PresetFilters
  notice: 'unavailable' | 'ignored' | null
}
type Transition =
  | { kind: 'preset'; id: string }
  | { kind: 'scope'; scope: PresetScope }
  | { kind: 'route' }

export function LogFilterPresetsProvider(props: { children: ReactNode }) {
  const params = route.useParams()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const { viewAccess, isAdminView } = useLogsViewScope()
  if (params.section !== 'common' || !userId) {
    return <LogPresetContext value={null}>{props.children}</LogPresetContext>
  }
  return (
    <CommonPresetEditor
      key={`${userId}:${viewAccess}`}
      userId={userId}
      scope={isAdminView ? 'all' : 'self'}
    >
      {props.children}
    </CommonPresetEditor>
  )
}

function CommonPresetEditor(props: {
  children: ReactNode
  userId: number
  scope: PresetScope
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const { setViewScope } = useLogsViewScope()
  const key = [
    'log-filter-presets',
    props.userId,
    'common',
    props.scope,
  ] as const
  const query = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => readPresets(props.scope, signal),
    refetchOnMount: 'always',
    refetchOnWindowFocus: 'always',
    meta: { errorToast: false },
  })
  const [state, setState] = useState<EditorState>(() => {
    const draft = draftFromSearch(search, props.scope)
    const selected = search.preset === 'system' ? 'system' : 'temporary'
    return {
      draft,
      selected,
      baseline: null,
      origin: savedFilters(draft, props.scope),
      notice: null,
      ready: hasExplicitLogFilters(search) || selected === 'system',
      initialized: false,
      applied: {
        ...draftSearch(draft, selected, props.scope),
        page: search.page,
        pageSize: search.pageSize,
      },
    }
  })
  const stateRef = useRef(state)
  stateRef.current = state
  const [nameMode, setNameMode] = useState<'create' | 'rename' | null>(null)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [pending, setPending] = useState<Transition | null>(null)
  const [saveThenSwitch, setSaveThenSwitch] = useState(false)
  const mounted = useRef(true)
  const epoch = useRef(0)
  const busyRef = useRef(false)
  const handledSearch = useRef<string | null>(null)
  const retrySource = useRef<string | null>(null)
  const failedInitialization = useRef<{
    sourceKey: string
    ticket: number
    initial: EditorState
  } | null>(null)
  const internalNavigation = useRef(false)
  const [initAttempt, setInitAttempt] = useState(0)
  const normalizedDraft = savedFilters(state.draft, props.scope)
  const dirty = !samePresetFilters(
    normalizedDraft,
    state.baseline?.filters ?? state.origin
  )
  const blocker = useBlocker({
    shouldBlockFn: ({ current, next }) =>
      !internalNavigation.current &&
      dirty &&
      (current.pathname !== next.pathname ||
        logFilterSearchKey({
          ...(current.search as LogFilterSearch),
          page: 1,
          pageSize: undefined,
        }) !==
          logFilterSearchKey({
            ...(next.search as LogFilterSearch),
            page: 1,
            pageSize: undefined,
          })),
    withResolver: true,
    enableBeforeUnload: dirty,
  })
  const mutation = useMutation({
    mutationFn: (work: () => Promise<void>) => work(),
    onError: (error) => {
      handleServerError(error)
      void client.invalidateQueries({ queryKey: key })
    },
    meta: { errorToast: false },
  })

  useEffect(() => {
    mounted.current = true
    handledSearch.current = null
    return () => {
      mounted.current = false
    }
  }, [])

  function ensureCurrent(ticket: number) {
    if (!mounted.current || ticket !== epoch.current) {
      throw new DOMException('Operation superseded', 'AbortError')
    }
  }

  async function commit(
    next: EditorState,
    ticket: number,
    replace = false,
    preserveDraft = false
  ) {
    ensureCurrent(ticket)
    const previousKey = handledSearch.current
    handledSearch.current = logFilterSearchKey(next.applied)
    internalNavigation.current = true
    try {
      await navigate({ search: next.applied, replace })
      ensureCurrent(ticket)
      const committed = preserveDraft
        ? { ...next, draft: stateRef.current.draft }
        : next
      setState(committed)
      stateRef.current = committed
    } catch (error) {
      handledSearch.current = previousKey
      throw error
    } finally {
      internalNavigation.current = false
    }
  }

  async function finishInitialization(
    catalog: PresetCollection,
    initial: EditorState,
    ticket: number
  ) {
    const explicit = hasExplicitLogFilters(search)
    const requested =
      search.preset && /^\d+$/.test(search.preset)
        ? catalog.items.find((item) => item.id === Number(search.preset))
        : undefined
    const fallback = catalog.items.find(
      (item) => item.id === catalog.preference.default_preset_id
    )
    let baseline = requested ?? null
    let selected = requested ? String(requested.id) : 'temporary'
    let restored = stateRef.current.draft
    if (!explicit) {
      if (search.preset === 'system' || search.preset === 'temporary') {
        baseline = null
      } else baseline = requested ?? fallback ?? null
      selected = baseline ? String(baseline.id) : 'system'
      restored = draftFromPreset(baseline?.filters ?? {}, props.scope)
    } else if (search.preset === 'system') selected = 'system'
    const unavailable = Boolean(
      search.preset &&
      !['system', 'temporary'].includes(search.preset) &&
      !requested
    )
    let notice: EditorState['notice'] = null
    if (unavailable) notice = 'unavailable'
    else if (baseline?.ignored_fields?.length) notice = 'ignored'
    const next: EditorState = {
      ready: true,
      initialized: true,
      baseline,
      origin: explicit ? initial.origin : (baseline?.filters ?? {}),
      selected,
      draft: restored,
      notice,
      applied: explicit
        ? { ...initial.applied, preset: selected }
        : {
            ...draftSearch(restored, selected, props.scope),
            page: search.page,
            pageSize: search.pageSize,
          },
    }
    try {
      await commit(next, ticket, true, explicit)
    } catch (error) {
      handleServerError(error)
    }
  }

  // URL changes initialize a new query, while focus refreshes only update the
  // remote catalog. Never replace a draft's content/version from the catalog.
  const sourceKey = logFilterSearchKey(search)
  useEffect(() => {
    if (handledSearch.current === sourceKey) return
    const current = stateRef.current
    if (
      handledSearch.current !== null &&
      current.ready &&
      logFilterSearchKey({ ...search, page: 1, pageSize: undefined }) ===
        logFilterSearchKey({ ...current.applied, page: 1, pageSize: undefined })
    ) {
      handledSearch.current = sourceKey
      setState((previous) => ({
        ...previous,
        applied: {
          ...previous.applied,
          page: search.page,
          pageSize: search.pageSize,
        },
      }))
      return
    }
    handledSearch.current = sourceKey
    const ticket = ++epoch.current
    failedInitialization.current = null
    const explicit = hasExplicitLogFilters(search)
    const draft = draftFromSearch(search, props.scope)
    const initial: EditorState = {
      ready: explicit || search.preset === 'system',
      initialized: false,
      draft,
      origin: savedFilters(draft, props.scope),
      applied: {
        ...draftSearch(draft, search.preset ?? 'temporary', props.scope),
        page: search.page,
        pageSize: search.pageSize,
      },
      selected: search.preset === 'system' ? 'system' : 'temporary',
      baseline: null,
      notice: null,
    }
    if (explicit && retrySource.current === sourceKey) {
      initial.draft = current.draft
      initial.origin = current.origin
      initial.applied = current.applied
    }
    retrySource.current = null
    setState(initial)
    stateRef.current = initial
    void (async () => {
      const result = await query.refetch()
      if (!mounted.current || ticket !== epoch.current) return
      if (!result.isSuccess) {
        failedInitialization.current = { sourceKey, ticket, initial }
        return
      }
      await finishInitialization(result.data, initial, ticket)
    })()
    // Initialization is driven by URL/account scope, not catalog refetches.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sourceKey, initAttempt])

  useEffect(() => {
    const failed = failedInitialization.current
    if (!query.isSuccess || !failed || failed.sourceKey !== sourceKey) return
    failedInitialization.current = null
    if (failed.ticket !== epoch.current || stateRef.current.initialized) return
    void finishInitialization(query.data, failed.initial, failed.ticket)
    // Recovery only completes the failed initialization; catalog updates must
    // not replace an already initialized editor's draft or version baseline.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query.isSuccess, query.dataUpdatedAt, sourceKey])

  useEffect(() => {
    if (
      mutation.isPending ||
      !query.isSuccess ||
      !state.ready ||
      !state.baseline
    ) {
      return
    }
    if (!query.data.items.some((item) => item.id === state.baseline?.id)) {
      setDeleteOpen(false)
      setState((current) => ({
        ...current,
        selected: 'temporary',
        baseline: null,
        notice: 'unavailable',
      }))
    }
  }, [
    mutation.isPending,
    query.isSuccess,
    query.data,
    state.ready,
    state.baseline,
  ])

  async function run(
    work: (ticket: number) => Promise<void>
  ): Promise<boolean> {
    if (busyRef.current) return false
    busyRef.current = true
    const ticket = ++epoch.current
    try {
      await mutation.mutateAsync(() => work(ticket))
      return true
    } catch {
      return false
    } finally {
      busyRef.current = false
    }
  }

  async function refreshCatalog(ticket: number) {
    await client.invalidateQueries({ queryKey: key })
    ensureCurrent(ticket)
  }

  async function saveCurrent(ticket: number, name?: string, rename = false) {
    const current = stateRef.current
    const filters = savedFilters(current.draft, props.scope)
    let saved: LogFilterPreset
    if (rename) {
      if (!current.baseline) {
        throw new Error(t('This filter preset is no longer available.'))
      }
      saved = await updatePreset(props.scope, current.baseline.id, {
        version: current.baseline.version,
        name,
      })
    } else if (name !== undefined) {
      saved = await createPreset(props.scope, name, filters)
    } else if (current.baseline) {
      saved = await updatePreset(props.scope, current.baseline.id, {
        version: current.baseline.version,
        filters,
      })
    } else throw new Error(t('Choose a name for this filter preset.'))
    ensureCurrent(ticket)
    client.setQueryData<PresetCollection>(key, (previous) =>
      previous
        ? {
            ...previous,
            items: [
              ...previous.items.filter((item) => item.id !== saved.id),
              saved,
            ].sort((a, b) => a.id - b.id),
          }
        : previous
    )
    setState((previous) => ({
      ...previous,
      baseline: saved,
      origin: saved.filters,
      selected: String(saved.id),
      notice: null,
    }))
    stateRef.current = {
      ...current,
      baseline: saved,
      origin: saved.filters,
      selected: String(saved.id),
      notice: null,
    }
    await refreshCatalog(ticket)
    toast.success(t('Filter preset saved'))
  }

  async function applySelected(id: string, ticket: number) {
    const baseline =
      id === 'system' ? null : await readPreset(props.scope, Number(id))
    ensureCurrent(ticket)
    if (baseline) {
      client.setQueryData<PresetCollection>(key, (previous) =>
        previous
          ? {
              ...previous,
              items: previous.items.map((item) =>
                item.id === baseline.id && item.version <= baseline.version
                  ? baseline
                  : item
              ),
            }
          : previous
      )
    }
    const draft = draftFromPreset(baseline?.filters ?? {}, props.scope)
    await commit(
      {
        ready: true,
        initialized: true,
        baseline,
        origin: baseline?.filters ?? {},
        selected: id,
        draft,
        applied: draftSearch(draft, id, props.scope),
        notice: baseline?.ignored_fields?.length ? 'ignored' : null,
      },
      ticket
    )
    await Promise.all([
      client.invalidateQueries({ queryKey: ['logs'] }),
      client.invalidateQueries({ queryKey: ['usage-logs-stats'] }),
    ])
  }

  async function transition(target: Transition, ticket: number) {
    ensureCurrent(ticket)
    if (target.kind === 'preset') await applySelected(target.id, ticket)
    else if (target.kind === 'scope') {
      const previousKey = handledSearch.current
      handledSearch.current = logFilterSearchKey({})
      internalNavigation.current = true
      try {
        await navigate({ search: {}, replace: true })
        ensureCurrent(ticket)
        setViewScope(target.scope)
      } catch (error) {
        handledSearch.current = previousKey
        throw error
      } finally {
        internalNavigation.current = false
      }
    } else blocker.proceed?.()
    if (mounted.current) setPending(null)
  }

  function requestTransition(target: Transition) {
    if (mutation.isPending) return
    if (dirty) setPending(target)
    else void run((ticket) => transition(target, ticket))
  }

  const target =
    pending ??
    (blocker.status === 'blocked' ? ({ kind: 'route' } as const) : null)
  const value: LogPresetContextValue = {
    ...state,
    loading: query.isPending || (!state.initialized && !query.isError),
    busy: mutation.isPending,
    dialogOpen: Boolean(nameMode || deleteOpen || target),
    error: query.error,
    collection: query.data,
    dirty,
    updated: Boolean(
      state.baseline &&
      query.data?.items.some(
        (item) =>
          item.id === state.baseline?.id &&
          item.version !== state.baseline.version
      )
    ),
    change: (field, entry) =>
      setState((current) => ({
        ...current,
        draft: {
          ...current.draft,
          filters: { ...current.draft.filters, [field]: entry },
        },
      })),
    changeType: (logType) =>
      setState((current) => ({
        ...current,
        draft: { ...current.draft, logType },
      })),
    apply: (filters?: CommonLogFilters) => {
      if (query.isPending || (!state.initialized && !query.isError)) {
        return Promise.resolve(false)
      }
      return run(async (ticket) => {
        const current = stateRef.current
        const draft = filters ? { ...current.draft, filters } : current.draft
        await commit(
          {
            ...current,
            draft,
            ready: true,
            applied: draftSearch(draft, current.selected, props.scope),
          },
          ticket
        )
        await Promise.all([
          client.invalidateQueries({ queryKey: ['logs'] }),
          client.invalidateQueries({ queryKey: ['usage-logs-stats'] }),
        ])
      })
    },
    reset: () =>
      run(async (ticket) => {
        let selected = stateRef.current.selected
        if (selected === 'temporary') {
          const latest = await readPresets(props.scope)
          ensureCurrent(ticket)
          client.setQueryData(key, latest)
          selected = latest.preference.default_preset_id
            ? String(latest.preference.default_preset_id)
            : 'system'
        }
        await applySelected(selected, ticket)
      }),
    select: (id) => {
      if (id !== 'temporary') requestTransition({ kind: 'preset', id })
    },
    save: () => {
      if (state.baseline) void run((ticket) => saveCurrent(ticket))
      else setNameMode('create')
    },
    saveAs: () => setNameMode('create'),
    rename: () => setNameMode('rename'),
    remove: () => setDeleteOpen(true),
    setDefault: () => {
      void run(async (ticket) => {
        if (!query.data) return
        if (stateRef.current.selected === 'temporary') {
          throw new Error(t('This filter preset is no longer available.'))
        }
        const preference = await setDefaultPreset(
          props.scope,
          stateRef.current.baseline?.id ?? null,
          query.data.preference.version
        )
        ensureCurrent(ticket)
        client.setQueryData<PresetCollection>(key, (previous) =>
          previous ? { ...previous, preference } : previous
        )
        await refreshCatalog(ticket)
        toast.success(t('Default filter preset updated'))
      })
    },
    changeScope: (scope) => {
      if (scope !== props.scope) requestTransition({ kind: 'scope', scope })
    },
    retry: () => {
      if (!state.initialized) {
        retrySource.current = sourceKey
        handledSearch.current = null
        setInitAttempt((attempt) => attempt + 1)
      } else void query.refetch()
    },
    useSystemDefault: () => {
      void run((ticket) => applySelected('system', ticket))
    },
  }

  return (
    <LogPresetContext value={value}>
      {props.children}
      {nameMode && (
        <PresetNameDialog
          name={nameMode === 'rename' ? (state.baseline?.name ?? '') : ''}
          rename={nameMode === 'rename'}
          busy={mutation.isPending}
          onClose={() => {
            setNameMode(null)
            if (saveThenSwitch) {
              setPending(null)
              blocker.reset?.()
              setSaveThenSwitch(false)
            }
          }}
          onSave={async (name) => {
            const success = await run(async (ticket) => {
              await saveCurrent(ticket, name, nameMode === 'rename')
              if (saveThenSwitch && target) await transition(target, ticket)
            })
            if (success && mounted.current) {
              setNameMode(null)
              setSaveThenSwitch(false)
            }
          }}
        />
      )}
      <ConfirmDialog
        open={Boolean(target) && !nameMode}
        onOpenChange={(open) => {
          if (!open && !mutation.isPending) {
            setPending(null)
            blocker.reset?.()
          }
        }}
        title={t('Unsaved changes')}
        desc={t('Save your filter changes before switching, or discard them.')}
        confirmText={t('Discard and switch')}
        isLoading={mutation.isPending}
        handleConfirm={() => {
          if (target) void run((ticket) => transition(target, ticket))
        }}
      >
        <Button
          disabled={mutation.isPending || query.isError || query.isPending}
          onClick={() => {
            if (!state.baseline) {
              setSaveThenSwitch(true)
              setNameMode('create')
            } else if (target) {
              void run(async (ticket) => {
                await saveCurrent(ticket)
                await transition(target, ticket)
              })
            }
          }}
        >
          {t('Save and switch')}
        </Button>
      </ConfirmDialog>
      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={(open) => {
          if (!mutation.isPending) setDeleteOpen(open)
        }}
        title={t('Delete filter preset?')}
        desc={t(
          'This deletes only the selected preset. Your other presets are kept.'
        )}
        confirmText={t('Delete')}
        destructive
        isLoading={mutation.isPending}
        handleConfirm={() => {
          void run(async (ticket) => {
            const baseline = stateRef.current.baseline
            if (!baseline) return
            const preference = await deletePreset(
              props.scope,
              baseline.id,
              baseline.version
            )
            ensureCurrent(ticket)
            client.setQueryData<PresetCollection>(key, (previous) =>
              previous
                ? {
                    ...previous,
                    preference,
                    items: previous.items.filter(
                      (item) => item.id !== baseline.id
                    ),
                  }
                : previous
            )
            setState((current) => ({
              ...current,
              baseline: null,
              selected: 'temporary',
            }))
            stateRef.current = {
              ...stateRef.current,
              baseline: null,
              selected: 'temporary',
            }
            setDeleteOpen(false)
            await applySelected(
              preference.default_preset_id
                ? String(preference.default_preset_id)
                : 'system',
              ticket
            )
            await refreshCatalog(ticket)
          })
        }}
      />
    </LogPresetContext>
  )
}
