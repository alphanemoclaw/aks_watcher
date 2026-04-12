// ─────────────────────────────────────────────────────────────────────────────
// AKS Cluster Dashboard — React + TypeScript + Tailwind
// ─────────────────────────────────────────────────────────────────────────────

import { useEffect, useRef, useState } from 'react'

// ─── Types ───────────────────────────────────────────────────────────────────

interface ClusterSummary {
  name: string
  resource_group: string
  location: string
  kubernetes_version: string
  provisioning_state: string
  power_state: string
  is_live: boolean
  set_live_at: string | null
  planned_live_at: string | null
}

interface LiveStatusPayload {
  cluster_name: string
  resource_group: string
  is_live: boolean
  planned_live_at: string | null
}

const API_BASE  = import.meta.env.VITE_API_BASE_URL ?? ''
const API_URL   = API_BASE + '/aks-watcher/api/clusters/summary'
const LIVE_URL  = API_BASE + '/aks-watcher/api/clusters/live-status'

const POLL_INTERVAL_MS = 60_000

// ─── StatusBadge ─────────────────────────────────────────────────────────────

type BadgeVariant = 'green' | 'yellow' | 'red' | 'gray' | 'blue' | 'purple'

const BADGE_CLASSES: Record<BadgeVariant, string> = {
  green:  'bg-green-100  text-green-800  ring-green-500/30',
  yellow: 'bg-yellow-100 text-yellow-800 ring-yellow-500/30',
  red:    'bg-red-100    text-red-800    ring-red-500/30',
  gray:   'bg-gray-100   text-gray-600   ring-gray-400/30',
  blue:   'bg-blue-100   text-blue-800   ring-blue-500/30',
  purple: 'bg-purple-100 text-purple-800 ring-purple-500/30',
}

const DOT_CLASSES: Record<BadgeVariant, string> = {
  green:  'bg-green-500',
  yellow: 'bg-yellow-500',
  red:    'bg-red-500',
  gray:   'bg-gray-400',
  blue:   'bg-blue-500',
  purple: 'bg-purple-500',
}

function StatusBadge({ label, variant }: { label: string; variant: BadgeVariant }) {
  return (
    <span className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset ${BADGE_CLASSES[variant]}`}>
      <span className={`h-1.5 w-1.5 rounded-full ${DOT_CLASSES[variant]}`} />
      {label}
    </span>
  )
}

function powerStateBadge(state: string): BadgeVariant {
  switch (state.toLowerCase()) {
    case 'running': return 'green'
    case 'stopped': return 'gray'
    default:        return 'blue'
  }
}

function provisioningStateBadge(state: string): BadgeVariant {
  switch (state.toLowerCase()) {
    case 'succeeded': return 'green'
    case 'failed':    return 'red'
    case 'updating':
    case 'creating':
    case 'deleting':  return 'yellow'
    default:          return 'blue'
  }
}

function formatDate(iso: string | null): string {
  if (!iso) return '—'
  return new Date(iso).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
}

// ─── LiveStatusModal ─────────────────────────────────────────────────────────

interface LiveStatusModalProps {
  cluster: ClusterSummary
  onClose: () => void
  onSave: (payload: LiveStatusPayload) => Promise<void>
}

function LiveStatusModal({ cluster, onClose, onSave }: LiveStatusModalProps) {
  const [isLive, setIsLive]               = useState(cluster.is_live)
  const [plannedLiveAt, setPlannedLiveAt] = useState(
    cluster.planned_live_at ? cluster.planned_live_at.slice(0, 10) : ''
  )
  const [saving, setSaving] = useState(false)

  const handleSave = async () => {
    setSaving(true)
    await onSave({
      cluster_name:   cluster.name,
      resource_group: cluster.resource_group,
      is_live:        isLive,
      planned_live_at: plannedLiveAt ? plannedLiveAt : null,
    })
    setSaving(false)
    onClose()
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
      <div className="w-full max-w-md rounded-2xl bg-white shadow-xl">

        {/* Header */}
        <div className="flex items-center justify-between border-b border-gray-100 px-6 py-4">
          <div>
            <h2 className="text-base font-semibold text-gray-900">{cluster.name}</h2>
            <p className="text-xs text-gray-500">{cluster.resource_group}</p>
          </div>
          <button
            onClick={onClose}
            className="rounded-lg p-1.5 text-gray-400 hover:bg-gray-100 hover:text-gray-600"
          >
            <svg className="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
              <path fillRule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clipRule="evenodd" />
            </svg>
          </button>
        </div>

        {/* Body */}
        <div className="space-y-5 px-6 py-5">

          {/* Live toggle */}
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium text-gray-700">Production live</p>
              <p className="text-xs text-gray-400">Mark this cluster as actively serving production traffic</p>
            </div>
            <button
              role="switch"
              aria-checked={isLive}
              onClick={() => setIsLive(v => !v)}
              className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors focus:outline-none focus:ring-2 focus:ring-purple-500 focus:ring-offset-2 ${isLive ? 'bg-purple-600' : 'bg-gray-200'}`}
            >
              <span className={`inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform ${isLive ? 'translate-x-6' : 'translate-x-1'}`} />
            </button>
          </div>

          {/* Set live at (read-only) */}
          {cluster.set_live_at && (
            <div>
              <p className="text-xs font-medium text-gray-400">Set live on</p>
              <p className="mt-0.5 text-sm text-gray-700">{formatDate(cluster.set_live_at)}</p>
            </div>
          )}

          {/* Planned live date */}
          <div>
            <label className="block text-sm font-medium text-gray-700" htmlFor="planned-date">
              Planned go-live date
            </label>
            <input
              id="planned-date"
              type="date"
              value={plannedLiveAt}
              onChange={e => setPlannedLiveAt(e.target.value)}
              className="mt-1.5 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-purple-500 focus:outline-none focus:ring-1 focus:ring-purple-500"
            />
          </div>
        </div>

        {/* Footer */}
        <div className="flex justify-end gap-3 border-t border-gray-100 px-6 py-4">
          <button
            onClick={onClose}
            className="rounded-lg border border-gray-200 px-4 py-2 text-sm font-medium text-gray-600 hover:bg-gray-50"
          >
            Cancel
          </button>
          <button
            onClick={handleSave}
            disabled={saving}
            className="rounded-lg bg-purple-600 px-4 py-2 text-sm font-medium text-white shadow-sm hover:bg-purple-700 disabled:opacity-50"
          >
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>

      </div>
    </div>
  )
}

// ─── ClusterCard ─────────────────────────────────────────────────────────────

function ClusterCard({ cluster, onClick }: { cluster: ClusterSummary; onClick: () => void }) {
  return (
    <div
      onClick={onClick}
      className="flex cursor-pointer flex-col gap-4 rounded-xl border border-gray-200 bg-white p-5 shadow-sm transition hover:shadow-md hover:border-purple-200"
    >
      {/* Header row */}
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="truncate text-base font-semibold text-gray-900">{cluster.name}</p>
          <p className="mt-0.5 truncate text-xs text-gray-500">{cluster.resource_group}</p>
        </div>
        <div className="flex flex-col items-end gap-1">
          <StatusBadge label={cluster.power_state} variant={powerStateBadge(cluster.power_state)} />
          {cluster.is_live && <StatusBadge label="Live" variant="purple" />}
        </div>
      </div>

      {/* Metadata grid */}
      <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-sm">
        <MetaItem label="Region"      value={cluster.location} />
        <MetaItem label="K8s Version" value={cluster.kubernetes_version} />
        {cluster.is_live && cluster.set_live_at && <MetaItem label="Live since" value={formatDate(cluster.set_live_at)} />}
        {!cluster.is_live && cluster.planned_live_at && <MetaItem label="Planned live" value={formatDate(cluster.planned_live_at)} />}
      </dl>

      {/* Footer */}
      <div className="flex items-center gap-2 border-t border-gray-100 pt-3">
        <span className="text-xs text-gray-500">Provisioning</span>
        <StatusBadge label={cluster.provisioning_state} variant={provisioningStateBadge(cluster.provisioning_state)} />
      </div>
    </div>
  )
}

function MetaItem({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs font-medium text-gray-400">{label}</dt>
      <dd className="mt-0.5 font-medium text-gray-700">{value}</dd>
    </div>
  )
}

// ─── ClusterGrid ─────────────────────────────────────────────────────────────

interface ClusterGridProps {
  clusters: ClusterSummary[]
  onCardClick: (c: ClusterSummary) => void
}

function ClusterGrid({ clusters, onCardClick }: ClusterGridProps) {
  if (clusters.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center rounded-xl border border-dashed border-gray-300 bg-gray-50 py-20 text-center">
        <span className="text-4xl">☸</span>
        <p className="mt-3 text-sm font-medium text-gray-600">No clusters found</p>
        <p className="mt-1 text-xs text-gray-400">Make sure your subscription / resource group contains AKS clusters.</p>
      </div>
    )
  }
  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
      {clusters.map((c) => (
        <ClusterCard key={c.name + c.resource_group} cluster={c} onClick={() => onCardClick(c)} />
      ))}
    </div>
  )
}

// ─── StatsBar ────────────────────────────────────────────────────────────────

function StatsBar({ clusters }: { clusters: ClusterSummary[] }) {
  const running = clusters.filter(c => c.power_state.toLowerCase()        === 'running').length
  const stopped = clusters.filter(c => c.power_state.toLowerCase()        === 'stopped').length
  const failed  = clusters.filter(c => c.provisioning_state.toLowerCase() === 'failed').length
  const live    = clusters.filter(c => c.is_live).length

  const stats = [
    { label: 'Total Clusters', value: clusters.length, color: 'text-gray-900'   },
    { label: 'Running',        value: running,          color: 'text-green-600'  },
    { label: 'Stopped',        value: stopped,          color: 'text-gray-500'   },
    { label: 'Failed',         value: failed,           color: 'text-red-600'    },
    { label: 'Live',           value: live,             color: 'text-purple-600' },
  ]

  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-5">
      {stats.map(s => (
        <div key={s.label} className="rounded-xl border border-gray-200 bg-white px-5 py-4 shadow-sm">
          <p className="text-xs font-medium text-gray-500">{s.label}</p>
          <p className={`mt-1 text-3xl font-bold tabular-nums ${s.color}`}>{s.value}</p>
        </div>
      ))}
    </div>
  )
}

// ─── Header ──────────────────────────────────────────────────────────────────

function Header({ lastRefreshed, isRefreshing, onRefresh }: { lastRefreshed: Date | null; isRefreshing: boolean; onRefresh: () => void }) {
  return (
    <header className="border-b border-gray-200 bg-white">
      <div className="mx-auto flex max-w-screen-xl items-center justify-between px-6 py-4">
        <div className="flex items-center gap-3">
          <span className="text-3xl leading-none">☸</span>
          <div>
            <h1 className="text-lg font-bold text-gray-900">AKS Cluster Dashboard</h1>
            <p className="text-xs text-gray-500">Azure Kubernetes Service monitor</p>
          </div>
        </div>
        <div className="flex items-center gap-4">
          {lastRefreshed && (
            <p className="hidden text-xs text-gray-400 sm:block">Updated {lastRefreshed.toLocaleTimeString()}</p>
          )}
          <button
            onClick={onRefresh}
            disabled={isRefreshing}
            className="flex items-center gap-1.5 rounded-lg border border-gray-200 bg-white px-3 py-1.5 text-sm font-medium text-gray-700 shadow-sm transition hover:bg-gray-50 disabled:opacity-50"
          >
            <svg className={`h-4 w-4 ${isRefreshing ? 'animate-spin' : ''}`} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            {isRefreshing ? 'Refreshing…' : 'Refresh'}
          </button>
        </div>
      </div>
    </header>
  )
}

// ─── LoadingSkeleton ──────────────────────────────────────────────────────────

function LoadingSkeleton() {
  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
      {Array.from({ length: 6 }).map((_, i) => (
        <div key={i} className="flex flex-col gap-4 rounded-xl border border-gray-200 bg-white p-5 shadow-sm">
          <div className="flex items-start justify-between">
            <div className="flex-1 space-y-2">
              <div className="h-4 w-3/4 animate-pulse rounded bg-gray-200" />
              <div className="h-3 w-1/2 animate-pulse rounded bg-gray-200" />
            </div>
            <div className="h-5 w-16 animate-pulse rounded-full bg-gray-200" />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <div className="h-3 w-10 animate-pulse rounded bg-gray-200" />
              <div className="h-4 w-16 animate-pulse rounded bg-gray-200" />
            </div>
            <div className="space-y-1.5">
              <div className="h-3 w-14 animate-pulse rounded bg-gray-200" />
              <div className="h-4 w-12 animate-pulse rounded bg-gray-200" />
            </div>
          </div>
          <div className="flex items-center gap-2 border-t border-gray-100 pt-3">
            <div className="h-3 w-20 animate-pulse rounded bg-gray-200" />
            <div className="h-5 w-20 animate-pulse rounded-full bg-gray-200" />
          </div>
        </div>
      ))}
    </div>
  )
}

// ─── ErrorBanner ─────────────────────────────────────────────────────────────

function ErrorBanner({ message }: { message: string }) {
  return (
    <div className="flex items-start gap-3 rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-800">
      <svg className="mt-0.5 h-5 w-5 shrink-0 text-red-500" viewBox="0 0 20 20" fill="currentColor">
        <path fillRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.28 7.22a.75.75 0 00-1.06 1.06L8.94 10l-1.72 1.72a.75.75 0 101.06 1.06L10 11.06l1.72 1.72a.75.75 0 101.06-1.06L11.06 10l1.72-1.72a.75.75 0 00-1.06-1.06L10 8.94 8.28 7.22z" clipRule="evenodd" />
      </svg>
      <div>
        <p className="font-semibold">Failed to reach the backend API</p>
        <p className="mt-0.5 text-red-700">{message}</p>
        <p className="mt-1 text-xs text-red-500">
          Make sure the Go backend is running and reachable at{' '}
          <code className="rounded bg-red-100 px-1">{API_URL}</code>
        </p>
      </div>
    </div>
  )
}

// ─── Dashboard ───────────────────────────────────────────────────────────────

function Dashboard() {
  const [clusters, setClusters]           = useState<ClusterSummary[]>([])
  const [loading, setLoading]             = useState(true)
  const [isRefreshing, setIsRefreshing]   = useState(false)
  const [error, setError]                 = useState<string | null>(null)
  const [lastRefreshed, setLastRefreshed] = useState<Date | null>(null)
  const [selectedCluster, setSelectedCluster] = useState<ClusterSummary | null>(null)

  const fetchRef = useRef<() => Promise<void>>(null!)

  fetchRef.current = async () => {
    try {
      const response = await fetch(API_URL)
      if (!response.ok) throw new Error(`HTTP ${response.status} — ${response.statusText}`)
      const data: ClusterSummary[] = await response.json()
      setClusters(data)
      setError(null)
      setLastRefreshed(new Date())
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    }
  }

  useEffect(() => {
    const run = async () => {
      setLoading(true)
      await fetchRef.current()
      setLoading(false)
    }
    run()
  }, [])

  useEffect(() => {
    const id = setInterval(async () => {
      setIsRefreshing(true)
      await fetchRef.current()
      setIsRefreshing(false)
    }, POLL_INTERVAL_MS)
    return () => clearInterval(id)
  }, [])

  const handleManualRefresh = async () => {
    if (isRefreshing) return
    setIsRefreshing(true)
    await fetchRef.current()
    setIsRefreshing(false)
  }

  const handleSaveLiveStatus = async (payload: LiveStatusPayload) => {
    try {
      const res = await fetch(LIVE_URL, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      // Refresh data so the card reflects the new status immediately.
      await fetchRef.current()
    } catch (err) {
      console.error('Failed to save live status:', err)
    }
  }

  return (
    <div className="min-h-screen bg-gray-50">
      <Header lastRefreshed={lastRefreshed} isRefreshing={isRefreshing} onRefresh={handleManualRefresh} />

      <main className="mx-auto max-w-screen-xl space-y-6 px-6 py-8">
        {clusters.length > 0 && <StatsBar clusters={clusters} />}
        {error && <ErrorBanner message={error} />}
        {loading ? (
          <LoadingSkeleton />
        ) : (
          <ClusterGrid clusters={clusters} onCardClick={setSelectedCluster} />
        )}
        <p className="text-center text-xs text-gray-400">
          Data refreshes automatically every {POLL_INTERVAL_MS / 1000} seconds. Click a cluster card to manage its live status.
        </p>
      </main>

      {selectedCluster && (
        <LiveStatusModal
          cluster={selectedCluster}
          onClose={() => setSelectedCluster(null)}
          onSave={handleSaveLiveStatus}
        />
      )}
    </div>
  )
}

// ─── App (root) ──────────────────────────────────────────────────────────────

export default function App() {
  return <Dashboard />
}
