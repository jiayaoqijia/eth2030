import Link from 'next/link';
import SectionHeading from '@/components/ui/SectionHeading';
import { FORKS } from '@/data/eips';
import { roadmapPhases } from '@/lib/data';

export default function RoadmapTimeline() {
  return (
    <section id="roadmap" className="py-20 md:py-28 px-4 bg-eth-surface/30">
      <SectionHeading title="The L1 Strawmap" subtitle="A living draft for Ethereum’s future, not a release schedule" />
      <div className="max-w-5xl mx-auto">
        <p className="text-eth-text/80 text-sm leading-7 mb-6">Selected milestones from the EF Architecture Strawmap dated November 2, 2026. Scope, ordering and dates can change through research and community coordination. These milestones describe Ethereum proposals; they do not indicate ETH2030 implementation status.</p>
        <div className="flex flex-wrap gap-3 mb-10 text-xs text-eth-teal">
          {['Fast L1', 'Gigagas L1', 'Teragas L2', 'Post-quantum L1', 'Private L1'].map(x => <span key={x} className="border border-eth-teal/30 rounded-full px-3 py-2">{x}</span>)}
        </div>
        <div className="grid md:grid-cols-2 gap-4">
          {roadmapPhases.map((phase) => {
            const fork = FORKS.find(({ name }) => name === phase.name);
            return (
              <Link
                key={phase.name}
                href={fork ? `/eips/#fork-${fork.id}` : '/eips/'}
                className="block rounded-xl border border-eth-purple/25 bg-eth-surface/60 p-6 hover:border-eth-purple/60"
              >
                <h3 className="text-lg font-bold text-eth-purple mb-3">{phase.name}</h3>
                <p className="text-eth-text text-sm leading-6">{phase.highlights.join(' · ')}</p>
                <p className="text-eth-dim text-xs mt-4 leading-5">{phase.note}</p>
              </Link>
            );
          })}
        </div>
        <p className="mt-8 text-sm text-eth-dim leading-7">
          The five north stars are long-term goals. I*–L* are placeholder fork names.{' '}
          <a className="text-eth-teal underline" href="https://strawmap.org/">View the current Strawmap</a>
          {' · '}
          <a className="text-eth-teal underline" href="https://blog.ethereum.org/2026/10/22/glamsterdam-hoodi-delay">Hoodi update</a>
          {' · '}
          <Link className="text-eth-teal underline" href="/pq-roadmap/">Post-quantum research notes</Link>
          {' · '}
          <Link className="text-eth-teal underline" href="/eips/">EIP Progress Tracker</Link>
        </p>
      </div>
    </section>
  );
}
