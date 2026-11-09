import type { Metadata } from 'next';
import Link from 'next/link';
import { notFound } from 'next/navigation';
import {
  EIPS,
  STATUS_META,
  LAYER_COLORS,
  forkByName,
  getEntry,
  STRAWMAP_REVISION,
} from '@/data/eips';

export function generateStaticParams() {
  return EIPS.map((e) => ({ id: e.id }));
}

export async function generateMetadata({
  params,
}: {
  params: Promise<{ id: string }>;
}): Promise<Metadata> {
  const { id } = await params;
  const e = getEntry(id);
  if (!e) return { title: 'EIP not found — ETH2030' };
  const name = e.number !== null ? `EIP-${e.number}` : e.title;
  return {
    title: `${name}: ${e.title} — ETH2030`,
    description: e.summary ?? `Implementation progress of ${name} in ETH2030.`,
  };
}

function Section({
  title,
  accent = 'text-eth-purple',
  children,
}: {
  title: string;
  accent?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="mt-8">
      <h2 className={`mb-3 text-sm font-bold uppercase tracking-wider ${accent}`}>{title}</h2>
      {children}
    </div>
  );
}

function FileList({ files, empty }: { files: string[]; empty: string }) {
  if (files.length === 0) return <div className="text-xs text-eth-dim">{empty}</div>;
  return (
    <ul className="space-y-1">
      {files.map((f) => (
        <li key={f} className="font-mono text-xs text-eth-teal">
          pkg/{f}
        </li>
      ))}
    </ul>
  );
}

export default async function EipPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const e = getEntry(id);
  if (!e) notFound();

  const fork = forkByName[e.fork];
  const meta = STATUS_META[e.status];
  const layerColor = LAYER_COLORS[e.layer];
  const name = e.number !== null ? `EIP-${e.number}` : 'Feature';
  const related = EIPS.filter(
    (x) => x.id !== e.id && x.fork === e.fork && (x.number !== null) === (e.number !== null),
  ).slice(0, 6);

  return (
    <main className="min-h-screen">
      <section className="mx-auto max-w-5xl px-6 pt-16 pb-24">
        <Link href="/eips" className="text-xs text-eth-dim hover:text-eth-purple">
          ← All EIPs
        </Link>

        {/* Header */}
        <div className="mt-4 flex flex-wrap items-center gap-3">
          <span
            className="rounded px-2 py-1 text-sm font-bold"
            style={{ color: layerColor, background: `${layerColor}18` }}
          >
            {name}
          </span>
          <span className="rounded border px-2 py-1 text-xs font-bold" style={{ color: layerColor, borderColor: `${layerColor}55` }}>
            {e.layer}
          </span>
          <span className="rounded border border-eth-dim/40 px-2 py-1 text-xs text-eth-dim">
            {fork.name} · {fork.year}
          </span>
          <span
            className="rounded border px-2 py-1 text-xs font-bold tracking-wider"
            style={{ color: meta.color, borderColor: meta.border, background: `${meta.color}11` }}
          >
            {meta.label}
          </span>
          {e.priority && (
            <span className="rounded border border-eth-pink/50 bg-eth-pink/10 px-2 py-1 text-xs font-bold text-eth-pink">
              GAP PRIORITY {e.priority}
            </span>
          )}
        </div>
        <h1 className="mt-4 text-2xl font-bold text-eth-text md:text-3xl">{e.title}</h1>
        {e.summary && <p className="mt-3 max-w-3xl text-sm text-eth-dim">{e.summary}</p>}

        {/* Strawmap placement */}
        {e.strawmap && (
          <div className="mt-4 rounded-lg border border-eth-purple/20 bg-eth-surface p-4">
            <div className="text-xs text-eth-dim">
              L1 Strawmap placement (revision {STRAWMAP_REVISION})
            </div>
            <div className="mt-2 flex flex-wrap items-center gap-2 text-sm">
              <span className="text-eth-text">“{e.strawmap.item}”</span>
              {e.strawmap.tag && (
                <span className="rounded border border-eth-teal/40 bg-eth-teal/10 px-1.5 py-0.5 text-[10px] font-bold text-eth-teal">
                  {e.strawmap.tag}
                </span>
              )}
              {e.strawmap.headliner && (
                <span className="rounded border border-eth-purple/50 bg-eth-purple/10 px-1.5 py-0.5 text-[10px] font-bold text-eth-purple">
                  ★ HEADLINER
                </span>
              )}
              {e.strawmap.offchain && (
                <span className="rounded border border-eth-dim/40 px-1.5 py-0.5 text-[10px] text-eth-dim">
                  offchain upgrade
                </span>
              )}
              {e.strawmap.crossLayer && (
                <span className="rounded border border-eth-pink/40 px-1.5 py-0.5 text-[10px] text-eth-pink">
                  cross-layer
                </span>
              )}
            </div>
          </div>
        )}

        {/* Completeness bar */}
        {e.completeness !== undefined && (
          <div className="mt-4 rounded-lg border border-eth-purple/20 bg-eth-surface p-4">
            <div className="flex items-center justify-between text-xs">
              <span className="text-eth-dim">Spec implementation completeness</span>
              <span className="font-bold" style={{ color: meta.color }}>
                {e.completeness}%
              </span>
            </div>
            <div className="mt-2 h-2 w-full rounded-full bg-eth-dim/20">
              <div
                className="h-2 rounded-full transition-all"
                style={{ width: `${e.completeness}%`, background: meta.color }}
              />
            </div>
          </div>
        )}

        {/* Spec links */}
        <Section title="Specification">
          <div className="flex flex-wrap gap-3 text-xs">
            {e.specUrl && (
              <a
                href={e.specUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="rounded border border-eth-blue/40 bg-eth-blue/10 px-2 py-1 text-eth-blue hover:border-eth-blue"
              >
                {e.specUrl.replace('https://eips.ethereum.org/', 'eips.ethereum.org/')} ↗
              </a>
            )}
            {e.specStatus && (
              <span className="rounded border border-eth-dim/40 px-2 py-1 text-eth-dim">
                upstream status: {e.specStatus}
              </span>
            )}
            <span className="rounded border border-eth-dim/40 px-2 py-1 text-eth-dim">
              category: {e.category}
            </span>
          </div>
        </Section>

        {/* Implemented (deep analysis) */}
        {e.implemented && e.implemented.length > 0 && (
          <Section title="Implemented" accent="text-eth-teal">
            <ul className="space-y-1">
              {e.implemented.map((g) => (
                <li key={g} className="text-sm text-eth-text">
                  <span className="mr-2 text-eth-teal">✓</span>
                  {g}
                </li>
              ))}
            </ul>
          </Section>
        )}

        {/* Gaps */}
        {e.gaps && e.gaps.length > 0 && (
          <Section title="Known Gaps vs. Spec" accent="text-eth-pink">
            <ul className="space-y-1">
              {e.gaps.map((g) => (
                <li key={g} className="text-sm text-eth-text">
                  <span className="mr-2 text-eth-pink">✗</span>
                  {g}
                </li>
              ))}
            </ul>
          </Section>
        )}

        {/* Code pointers */}
        <Section title="Implementation Files">
          <FileList files={e.implementation} empty="No implementation yet (planned)." />
        </Section>
        <Section title="Tests">
          <FileList files={e.tests} empty="No dedicated test files recorded (covered indirectly)." />
        </Section>

        {/* Related in same fork */}
        {related.length > 0 && (
          <Section title={`More in ${fork.name}`}>
            <div className="flex flex-wrap gap-2">
              {related.map((r) => (
                <Link
                  key={r.id}
                  href={`/eips/${r.id}`}
                  className="rounded border border-eth-purple/20 bg-eth-surface px-2 py-1 text-xs text-eth-dim hover:border-eth-purple/50 hover:text-eth-purple"
                >
                  {r.number !== null ? `EIP-${r.number}` : '◆'} {r.title}
                </Link>
              ))}
            </div>
          </Section>
        )}
      </section>
    </main>
  );
}
