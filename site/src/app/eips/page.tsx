'use client';
import { useMemo, useState } from 'react';
import Link from 'next/link';
import {
  EIPS,
  FORKS,
  NORTH_STARS,
  STATUS_META,
  LAYER_COLORS,
  STRAWMAP_REVISION,
  entriesByFork,
  type Layer,
  type Status,
} from '@/data/eips';

type LayerFilter = Layer | 'all';
type StatusFilter = Status | 'all';

const LAYERS: LayerFilter[] = ['all', 'CL', 'DL', 'EL', 'cross'];
const STATUSES: StatusFilter[] = ['all', 'complete', 'substantial', 'functional', 'referenced', 'partial', 'planned'];

function StatusBadge({ status }: { status: Status }) {
  const meta = STATUS_META[status];
  return (
    <span
      className="inline-block rounded border px-1.5 py-0.5 text-[10px] font-bold tracking-wider"
      style={{ color: meta.color, borderColor: meta.border, background: `${meta.color}11` }}
    >
      {meta.label}
    </span>
  );
}

function StrawmapTags({
  tag,
  headliner,
  offchain,
  crossLayer,
}: {
  tag?: 'SFI' | 'CFI' | 'EIP';
  headliner?: boolean;
  offchain?: boolean;
  crossLayer?: boolean;
}) {
  return (
    <span className="inline-flex flex-wrap gap-1">
      {headliner && (
        <span className="rounded border border-eth-purple/50 bg-eth-purple/10 px-1.5 py-0.5 text-[10px] font-bold text-eth-purple">
          ★ HEADLINER
        </span>
      )}
      {tag && (
        <span className="rounded border border-eth-teal/40 bg-eth-teal/10 px-1.5 py-0.5 text-[10px] font-bold text-eth-teal">
          {tag}
        </span>
      )}
      {crossLayer && (
        <span className="rounded border border-eth-pink/40 bg-eth-pink/10 px-1.5 py-0.5 text-[10px] text-eth-pink">
          cross-layer
        </span>
      )}
      {offchain && (
        <span className="rounded border border-eth-dim/40 bg-eth-dim/10 px-1.5 py-0.5 text-[10px] text-eth-dim">
          offchain
        </span>
      )}
    </span>
  );
}

function EipCard({ id }: { id: string }) {
  const e = EIPS.find((x) => x.id === id)!;
  const layerColor = LAYER_COLORS[e.layer];
  return (
    <Link
      href={`/eips/${e.id}`}
      className="group block rounded-lg border border-eth-purple/20 bg-eth-surface p-4 transition-all hover:border-eth-purple/50 hover:shadow-[0_0_14px_#8c8dfc33]"
    >
      <div className="mb-2 flex items-start justify-between gap-2">
        <div className="flex items-center gap-2">
          {e.number !== null ? (
            <span className="text-sm font-bold" style={{ color: layerColor }}>
              EIP-{e.number}
            </span>
          ) : (
            <span className="text-[10px] font-bold uppercase tracking-wider text-eth-dim">feature</span>
          )}
          <span
            className="rounded px-1.5 py-0.5 text-[10px] font-bold"
            style={{ color: layerColor, background: `${layerColor}18` }}
          >
            {e.layer}
          </span>
        </div>
        <StatusBadge status={e.status} />
      </div>
      <div className="mb-2 text-sm font-semibold text-eth-text group-hover:text-eth-purple">{e.title}</div>
      {e.strawmap && (
        <div className="mb-2">
          <StrawmapTags {...e.strawmap} />
        </div>
      )}
      {e.completeness !== undefined && (
        <div className="mt-2">
          <div className="flex items-center justify-between text-[10px] text-eth-dim">
            <span>spec completeness</span>
            <span>{e.completeness}%</span>
          </div>
          <div className="mt-1 h-1 w-full rounded-full bg-eth-dim/20">
            <div
              className="h-1 rounded-full"
              style={{ width: `${e.completeness}%`, background: STATUS_META[e.status].color }}
            />
          </div>
        </div>
      )}
      {e.priority && (
        <div className="mt-2 text-[10px] font-bold text-eth-pink">GAP PRIORITY: {e.priority}</div>
      )}
    </Link>
  );
}

export default function EipsPage() {
  const [layerFilter, setLayerFilter] = useState<LayerFilter>('all');
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');

  const groups = useMemo(() => {
    return entriesByFork()
      .map(({ fork, entries }) => ({
        fork,
        entries: entries.filter(
          (e) =>
            (layerFilter === 'all' || e.layer === layerFilter) &&
            (statusFilter === 'all' || e.status === statusFilter),
        ),
      }))
      .filter((g) => g.entries.length > 0);
  }, [layerFilter, statusFilter]);

  const stats = useMemo(() => {
    const eipCount = EIPS.filter((e) => e.number !== null).length;
    const featureCount = EIPS.length - eipCount;
    const byStatus = Object.fromEntries(
      (Object.keys(STATUS_META) as Status[]).map((s) => [s, EIPS.filter((e) => e.status === s).length]),
    ) as Record<Status, number>;
    return { eipCount, featureCount, byStatus };
  }, []);

  return (
    <main className="min-h-screen">
      <section className="mx-auto max-w-7xl px-6 pt-16 pb-6">
        <Link href="/" className="text-xs text-eth-dim hover:text-eth-purple">
          ← ETH2030
        </Link>
        <h1 className="mt-4 text-3xl font-bold text-eth-text md:text-4xl">
          EIP <span className="text-eth-purple">Progress Tracker</span>
        </h1>
        <p className="mt-3 max-w-3xl text-sm text-eth-dim">
          Per-EIP implementation progress in ETH2030, organized by the L1 Strawmap fork timeline
          (strawmap.org, revision {STRAWMAP_REVISION}). Status sources: repo traceability matrix,
          gap analyses, and the actual <code className="text-eth-purple">pkg/</code> tree — cross-checked
          by <code className="text-eth-purple">scripts/verify-eips.ts</code>.
        </p>

        {/* North stars */}
        <div className="mt-6 flex flex-wrap gap-2">
          <span className="text-xs text-eth-dim">North stars:</span>
          {NORTH_STARS.map((s) => (
            <span
              key={s.name}
              className="rounded border border-eth-dim/30 bg-eth-surface px-2 py-0.5 text-xs text-eth-text"
            >
              <span className="text-eth-teal">★ {s.name}</span>
              <span className="text-eth-dim"> — {s.desc}</span>
            </span>
          ))}
        </div>

        {/* Stats */}
        <div className="mt-6 grid grid-cols-2 gap-2 md:grid-cols-7">
          <div className="rounded-lg border border-eth-purple/20 bg-eth-surface p-3">
            <div className="text-xl font-bold text-eth-text">{stats.eipCount}</div>
            <div className="text-[10px] text-eth-dim">EIPs tracked</div>
          </div>
          <div className="rounded-lg border border-eth-purple/20 bg-eth-surface p-3">
            <div className="text-xl font-bold text-eth-text">{stats.featureCount}</div>
            <div className="text-[10px] text-eth-dim">roadmap features</div>
          </div>
          {(Object.keys(STATUS_META) as Status[]).map((s) => (
            <div key={s} className="rounded-lg border border-eth-purple/20 bg-eth-surface p-3">
              <div className="text-xl font-bold" style={{ color: STATUS_META[s].color }}>
                {stats.byStatus[s]}
              </div>
              <div className="text-[10px] text-eth-dim">{STATUS_META[s].label.toLowerCase()}</div>
            </div>
          ))}
        </div>

        {/* Filters */}
        <div className="mt-6 flex flex-wrap items-center gap-4">
          <div className="flex items-center gap-1.5">
            <span className="text-xs text-eth-dim">layer</span>
            {LAYERS.map((l) => (
              <button
                key={l}
                onClick={() => setLayerFilter(l)}
                className={`rounded border px-2 py-1 text-xs transition-colors ${
                  layerFilter === l
                    ? 'border-eth-purple bg-eth-purple/20 text-eth-purple'
                    : 'border-eth-dim/30 text-eth-dim hover:text-eth-text'
                }`}
              >
                {l === 'all' ? 'ALL' : l}
              </button>
            ))}
          </div>
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="text-xs text-eth-dim">status</span>
            {STATUSES.map((s) => (
              <button
                key={s}
                onClick={() => setStatusFilter(s)}
                className={`rounded border px-2 py-1 text-xs transition-colors ${
                  statusFilter === s
                    ? 'border-eth-purple bg-eth-purple/20 text-eth-purple'
                    : 'border-eth-dim/30 text-eth-dim hover:text-eth-text'
                }`}
              >
                {s === 'all' ? 'ALL' : STATUS_META[s as Status].label}
              </button>
            ))}
          </div>
        </div>

        {/* Fork jump nav */}
        <div className="mt-4 flex flex-wrap gap-2">
          {FORKS.map((f) => (
            <a
              key={f.id}
              href={`#fork-${f.id}`}
              className="text-xs text-eth-dim hover:text-eth-purple"
            >
              {f.name}
            </a>
          ))}
        </div>
      </section>

      {/* Fork sections */}
      <section className="mx-auto max-w-7xl px-6 pb-24">
        {groups.map(({ fork, entries }) => (
          <div key={fork.id} id={`fork-${fork.id}`} className="mt-12 scroll-mt-8">
            <div className="mb-4 flex flex-wrap items-baseline gap-3">
              <h2 className="text-xl font-bold text-eth-text">
                {fork.name} <span className="text-sm font-normal text-eth-dim">{fork.year}</span>
              </h2>
              <span className="text-xs text-eth-dim">{entries.length} items</span>
              {fork.headliners.map((h) => (
                <span
                  key={h.item}
                  className="rounded border px-1.5 py-0.5 text-[10px] font-bold"
                  style={{ color: LAYER_COLORS[h.layer], borderColor: `${LAYER_COLORS[h.layer]}55` }}
                >
                  ★ {h.layer} headliner: {h.item}
                </span>
              ))}
            </div>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
              {entries.map((e) => (
                <EipCard key={e.id} id={e.id} />
              ))}
            </div>
          </div>
        ))}
        {groups.length === 0 && (
          <div className="mt-16 text-center text-sm text-eth-dim">No entries match the filters.</div>
        )}
      </section>
    </main>
  );
}
