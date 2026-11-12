import type { CSSProperties } from 'react';
import Link from 'next/link';
import SectionHeading from '@/components/ui/SectionHeading';
import { roadmapStages, roadmapProgress, roadmapStatusNames } from '@/lib/roadmap';
import styles from './RoadmapTimeline.module.css';

export default function RoadmapTimeline() {
  return <section id="roadmap" className={styles.section}>
    <SectionHeading title="The L1 Strawmap" subtitle="Ethereum’s roadmap. ETH2030’s work on it." />
    <div className={styles.wrap}>
      <div className={styles.intro}>
        <p>Six proposed upgrades from the EF Architecture Strawmap, dated November 2, 2026. Explore the project&apos;s tracked work against the roadmap.</p>
        <Link href="/roadmap/" className={styles.detailLink}>Explore the full roadmap <span>↗</span></Link>
      </div>
      <div className={styles.cards} aria-label="Roadmap progress">
        {roadmapStages.map((stage, index) => {
          const { entries, counts, completePercentage } = roadmapProgress(stage.id);
          // Finished EIPs stay out of the homepage's work-in-progress list.
          const pending = entries.filter(entry => entry.status !== 'complete');
          const unique = [...new Map(pending.map(entry => [entry.strawmap?.item ?? entry.title, entry])).values()];
          return <Link href={`/roadmap/#fork-${stage.id}`} key={stage.id} className={styles.card} data-phase={stage.id} style={{ '--card-index': index, '--complete-pct': completePercentage ?? 0 } as CSSProperties}>
            <div className={styles.cardHead}><span className={styles.glyph}>{stage.glyph}</span><div><span className={styles.phaseIndex}>FORK 0{index + 1}</span><h3>{stage.name}</h3></div><span className={styles.arrow}>↗</span></div>
            <div className={styles.progress} role="img" aria-label={completePercentage === null ? 'No mapped tracker entries' : `${completePercentage}% of mapped tracker entries labelled complete`}>
              <div className={styles.ring}><strong>{completePercentage ?? '—'}{completePercentage !== null && <small>%</small>}</strong></div>
              <div className={styles.progressText}><span>TRACKER COMPLETE</span><strong>{counts.complete} <i>/</i> {entries.length}</strong><small>mapped entries</small></div>
            </div>
            <div className={styles.pendingTitle}><span>WORK IN THE TRACKER</span><span>{pending.length} entries</span></div>
            <div className={styles.pending}>
              {unique.slice(0, 3).map(entry => <div key={entry.id} data-status={entry.status} className={styles.pendingRow}><span>{entry.strawmap?.item ?? entry.title}</span><span>{roadmapStatusNames[entry.status]}</span></div>)}
              {unique.length === 0 && <p className={styles.empty}>No non-complete mapped entries in this snapshot.</p>}
            </div>
            <div className={styles.cardFooter}><span>{unique.length > 3 ? `+${unique.length - 3} more nodes` : 'View mapped nodes'}</span><span>FULL ROADMAP ↗</span></div>
          </Link>;
        })}
      </div>
      <p className={styles.note}>Percentages count mapped entries labelled Complete in the historical tracker; they are not independent conformance or production-readiness measurements. Unmapped EF nodes are excluded. I*–L* are placeholder names. <a href="https://strawmap.org/" target="_blank" rel="noreferrer">View the EF original ↗</a></p>
      <p className={styles.secondary}><Link href="/eips/">EIP Progress Tracker</Link><span>·</span><Link href="/pq-roadmap/">Post-quantum research notes</Link></p>
    </div>
  </section>;
}
