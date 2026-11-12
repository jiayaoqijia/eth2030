import type { CSSProperties } from 'react';
import Link from 'next/link';
import GlitchText from '@/components/ui/GlitchText';
import MatrixRain from '@/components/ui/MatrixRain';
import { type Layer } from '@/data/eips';
import { roadmapStages, roadmapStatuses, roadmapStatusNames, roadmapLayer, roadmapProgress, groupRoadmapNodes } from '@/lib/roadmap';
import styles from './roadmap.module.css';

const layerNames: Record<Layer, string> = { CL: 'Consensus layer', DL: 'Data layer', EL: 'Execution layer', cross: 'Cross-layer' };
const layerClasses: Record<Layer, string> = { CL: styles.cl, DL: styles.dl, EL: styles.el, cross: styles.cross };
const source = 'https://strawmap.org/';

function PhaseCard({ phase, index }: { phase: typeof roadmapStages[number]; index: number }) {
  const { entries, counts, completePercentage } = roadmapProgress(phase.id);
  const groups = groupRoadmapNodes(entries).map(group => ({ name: group[0].strawmap?.item ?? group[0].title, layer: roadmapLayer(group[0]), entries: group }));
  // Present an EF node even when the current project tracker has no mapping.
  if (phase.id === 'hegota') groups.push({ name: 'cryptography specs in Lean 4', layer: 'CL', entries: [] });
  return <article id={`fork-${phase.id}`} className={styles.card} style={{ '--card-index': index } as CSSProperties}>
    <div className={styles.cardGlow} aria-hidden="true" />
    <div className={styles.cardHead}>
      <div className={styles.cardCode}>{phase.glyph}</div>
      <div className={styles.cardTitle}><span className={styles.phaseIndex}>FORK 0{index + 1} <i /> EF STRAWMAP</span><h2>{phase.name}</h2><p>{phase.heading}</p></div>
      <div className={styles.cardTotal}><strong>{entries.length.toString().padStart(2, '0')}</strong><span>tracked<br />entries</span></div>
    </div>
    <p className={styles.cardDescription}>{phase.description}</p>
    <div className={styles.scoreRow}>
      <div className={styles.score} role="img" style={{ '--complete-pct': completePercentage ?? 0 } as CSSProperties} aria-label={completePercentage === null ? 'No mapped tracker entries' : `${completePercentage}% of mapped tracker entries are labelled complete`}>
        <div className={styles.scoreInner}><strong>{completePercentage ?? '—'}{completePercentage !== null && <span>%</span>}</strong><small>TRACKER COMPLETE</small></div>
      </div>
      <div className={styles.scoreCopy}><span>PROJECT TRACKER</span><strong>{counts.complete} <i>/</i> {entries.length}</strong><small>entries labelled complete</small></div>
    </div>
    <div className={styles.progressHeader}><span>TRACKER LABEL DISTRIBUTION</span><span>{entries.length} entries</span></div>
    <div className={styles.progressBar} role="img" aria-label={roadmapStatuses.filter(s => counts[s]).map(s => `${counts[s]} ${roadmapStatusNames[s]}`).join(', ')}>
      {roadmapStatuses.filter(status => counts[status] > 0).map(status => <span key={status} className={styles[status]} style={{ flexGrow: counts[status] }} />)}
    </div>
    <div className={styles.counts}>{roadmapStatuses.filter(status => counts[status] > 0).map(status => <span key={status}><i className={styles[status]} />{counts[status]} {roadmapStatusNames[status]}</span>)}</div>

    <div className={styles.nodes}>
      {(['CL', 'DL', 'EL', 'cross'] as Layer[]).map(layer => {
        const items = groups.filter(group => group.layer === layer);
        if (!items.length) return null;
        return <div className={styles.layer} key={layer}>
          <div className={`${styles.layerHeading} ${layerClasses[layer]}`}><span>{layer}</span><span>{layerNames[layer]}</span><i /></div>
          {items.map(group => {
            const entry = group.entries[0];
            return <div className={`${styles.node} ${group.entries.length > 2 ? styles.nodeDense : ''}`} key={`${group.layer}:${group.name}`}>
              <div className={styles.nodeHeading}><span>{group.name}</span><span className={styles.nodeMeta}>{group.entries.length > 1 ? `${group.entries.length} entries` : entry?.number ? `EIP-${entry.number}` : entry ? 'Research' : 'Not mapped'}</span></div>
              <div className={styles.nodeEntries}>{group.entries.length === 0 && <span className={styles.unmapped}>No tracker entry yet</span>}{group.entries.map(item => <span key={item.id} className={`${styles.entry} ${styles[`entry_${item.status}`]}`} title={`${item.title} — tracker: ${roadmapStatusNames[item.status]}; ${item.implementation.length ? 'code referenced' : 'no code listed'}; ${item.tests.length ? 'tests referenced' : 'no tests listed'}`}>
                <span className={styles.entryName}>{item.number ? `EIP-${item.number}` : roadmapStatusNames[item.status]}</span><span className={styles.entryStatus}>{item.number ? `${roadmapStatusNames[item.status]}${item.tests.length ? ' · tests' : ''}${item.id === 'eip-8077' ? ' · tentative map' : ''}` : item.tests.length ? 'tests referenced' : 'no tests listed'}</span>
              </span>)}</div>
            </div>;
          })}
        </div>;
      })}
    </div>
    <div className={styles.cardFooter}><span><i /> TRACKER SNAPSHOT</span><span>CODE &amp; TEST REFERENCES</span></div>
  </article>;
}

export default function RoadmapPage() {
  return <main className={styles.page}>
    <MatrixRain />
    <header className={styles.nav}><Link href="/" className={styles.brand}><span>◆</span> ETH2030 <i>/</i> Roadmap</Link><a href={source} target="_blank" rel="noreferrer" className={styles.sourceLink}>EF original ↗</a></header>
    <div className={styles.wrap}>
      <section className={styles.hero}>
        <div className={styles.heroGrid} aria-hidden="true" />
        <div className={styles.heroContent}>
          <div className={styles.overline}><span /> EF ARCHITECTURE STRAWMAP <i>/</i> NOV 02, 2026</div>
          <h1><GlitchText as="span" text="THE ROAD AHEAD" /></h1>
          <p>Ethereum&apos;s roadmap. <strong>ETH2030&apos;s work on it.</strong></p>
          <div className={styles.heroDescription}>Six proposed upgrades. Every mapped roadmap node, with the project&apos;s tracker status in view.</div>
          <div className={styles.legend}><span><i className={styles.dotCl} /> Consensus</span><span><i className={styles.dotDl} /> Data</span><span><i className={styles.dotEl} /> Execution</span><span><i className={styles.dotCross} /> Cross-layer</span></div>
        </div>
        <div className={styles.heroAside}><span>THE BIG PICTURE</span><strong>G* <i>→</i> L*</strong><p>Six upgrades. One evolving map.</p><a href={source} target="_blank" rel="noreferrer">View the original EF drawing ↗</a></div>
      </section>

      <div className={styles.intro}><div><span>01 / PROTOCOL ROADMAP</span><h2>Six upgrades. One view.</h2><p>Each card contains all mapped entries from the project tracker.</p></div><div className={styles.snapshot}><i /> TRACKER SNAPSHOT <b>NOV 2026</b></div></div>
      <section className={styles.cards} aria-label="Ethereum roadmap upgrades">{roadmapStages.map((phase, index) => <PhaseCard key={phase.id} phase={phase} index={index} />)}</section>
      <aside className={styles.caveat}><span>READING THE SIGNAL</span><p>Each percentage is the number of mapped entries labelled <strong>Complete</strong> in the historical project tracker divided by all mapped tracker entries in that card, rounded to the nearest whole percent. The unmapped EF node is excluded. These figures are not measured implementation completion, independent conformance, or production readiness. Colored bars show all tracker-label categories. EF&apos;s roadmap is a changing draft; I*–L* are placeholders. The older tracker places leanVM and ethp2p unification in different columns; this view follows the EF drawing. The longer-term column is outside these six cards.</p><a href="https://github.com/alt-research/eth2030/blob/master/STATUS.md" target="_blank" rel="noreferrer">Read project status ↗</a></aside>
      <footer className={styles.footer}><span>◆ ETH2030 <i>/</i> ROADMAP EXPLORER</span><a href={source}>SOURCE: EF ARCHITECTURE ↗</a></footer>
    </div>
  </main>;
}
