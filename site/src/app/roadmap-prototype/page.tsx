'use client';

import { useState } from 'react';
import Link from 'next/link';
import { EIPS, type EipEntry, type ForkId, type Layer } from '@/data/eips';
import styles from './roadmap.module.css';

const phases: { id: ForkId; name: string; glyph: string; heading: string; description: string }[] = [
  { id: 'glamsterdam', name: 'Glamsterdam', glyph: 'G', heading: 'A more capable L1', description: 'Block access lists and enshrined proposer-builder separation.' },
  { id: 'hegota', name: 'Hegotá', glyph: 'H', heading: 'A more open L1', description: 'Inclusion lists, quick slots and programmable transactions.' },
  { id: 'i', name: 'I*', glyph: 'I', heading: 'The next foundations', description: 'Decoupled consensus, binary state and post-quantum research.' },
  { id: 'j', name: 'J*', glyph: 'J', heading: 'Towards post-quantum', description: 'Attestations, sampling and proof systems for a new security model.' },
  { id: 'k', name: 'K*', glyph: 'K', heading: 'Proofs and state', description: 'Mandatory proofs and a path towards decentralized state.' },
  { id: 'l', name: 'L*', glyph: 'L', heading: 'The longer horizon', description: 'One-round finality, native rollups and protocol simplification.' },
];

const layerNames: Record<Layer, string> = { CL: 'Consensus', DL: 'Data', EL: 'Execution', cross: 'Cross-layer' };
const layerClasses: Record<Layer, string> = { CL: styles.cl, DL: styles.dl, EL: styles.el, cross: styles.cross };
const source = 'https://strawmap.org/';
const sourceImage = 'https://docs.google.com/drawings/d/1GkcGfQv9kxgrYQMyu0BYGcZya0gIDG_wQ7GsFJEsdzY/export/png';

// Preserve the positions in the current EF drawing even where the older
// repository tracker places these nodes in a different column.
function roadmapFork(entry: EipEntry): ForkId {
  if (entry.id === 'feature-leanvm-pq-proofs') return 'i';
  if (entry.id === 'feature-ethp2p-unification') return 'l';
  return entry.fork;
}

export default function RoadmapPrototype() {
  const [selected, setSelected] = useState<ForkId>('glamsterdam');
  const phase = phases.find(item => item.id === selected)!;
  const entries = EIPS.filter(item => roadmapFork(item) === selected && item.strawmap);
  const groups = new Map<string, EipEntry[]>();
  for (const entry of entries) {
    const key = `${entry.layer}:${entry.strawmap?.item}`;
    groups.set(key, [...(groups.get(key) ?? []), entry]);
  }

  return <main className={styles.page}>
    <header className={styles.nav}>
      <Link href="/" className={styles.brand}><span className={styles.mark}>◆</span> ETH2030 <span className={styles.brandSep}>/</span> <span>Roadmap</span></Link>
      <span className={styles.prototype}>Interactive prototype</span>
      <a className={styles.sourceLink} href={source} target="_blank" rel="noreferrer">EF original ↗</a>
    </header>

    <div className={styles.wrap}>
      <section className={styles.hero}>
        <div className={styles.heroText}>
          <div className={styles.overline}><span className={styles.signal} /> EF Architecture Strawmap · 02 Nov 2026</div>
          <h1>Ethereum&apos;s roadmap.<br /><em>Our work on it.</em></h1>
          <p>Six proposed upgrades, from today&apos;s near-term work to Ethereum&apos;s longer horizon. Explore the original roadmap nodes alongside ETH2030&apos;s tracked code and tests.</p>
          <div className={styles.legend}><span><i className={styles.dotCl} />Consensus</span><span><i className={styles.dotDl} />Data</span><span><i className={styles.dotEl} />Execution</span><span><i className={styles.dotCross} />Cross-layer</span></div>
        </div>
        <a href={source} target="_blank" rel="noreferrer" className={styles.mapPreview} aria-label="Open the Ethereum Foundation's original Strawmap">
          <div className={styles.previewTop}><span>THE ORIGINAL STRAWMAP</span><span>↗</span></div>
          {/* The image is served directly from the EF-maintained drawing. */}
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src={sourceImage} alt="Preview of the Ethereum Foundation's L1 Strawmap" />
          <div className={styles.previewBottom}>EF Architecture <span>Open source map ↗</span></div>
        </a>
      </section>

      <div className={styles.sectionTitle}><div><span className={styles.sectionNumber}>01</span><h2>The six upgrades</h2></div><p>Select a card to explore its roadmap nodes.</p></div>
      <section className={styles.cards} aria-label="Ethereum roadmap upgrades">
        {phases.map((item, index) => {
          const mapped = EIPS.filter(entry => roadmapFork(entry) === item.id && entry.strawmap);
          const highlights = (['CL', 'DL', 'EL', 'cross'] as Layer[]).map(layer => mapped.find(entry => entry.layer === layer)).filter(entry => entry !== undefined);
          return <button type="button" key={item.id} aria-pressed={selected === item.id} className={`${styles.card} ${selected === item.id ? styles.active : ''}`} onClick={() => setSelected(item.id)}>
            <div className={styles.cardTop}><span>0{index + 1} <span className={styles.cardSlash}>/</span> PROPOSED UPGRADE</span><span className={styles.cardArrow}>{selected === item.id ? '↗' : '↗'}</span></div>
            <div className={styles.cardGlyph} aria-hidden="true">{item.glyph}<span>*</span></div>
            <div className={styles.cardContent}><div className={styles.cardName}>{item.name}<span className={styles.cardLine} /></div><h3>{item.heading}</h3><p>{item.description}</p>
              <div className={styles.cardNodes}>{highlights.map(entry => <span className={`${styles.cardChip} ${layerClasses[entry.layer]}`} key={entry.id}>{entry.strawmap?.item}</span>)}</div>
              <div className={styles.cardFoot}><span>{mapped.length} mapped tracker entries</span><span>{selected === item.id ? 'Selected' : 'Explore'}</span></div>
            </div>
          </button>;
        })}
      </section>

      <section className={styles.detail} aria-live="polite">
        <div className={styles.detailIntro}><span className={styles.sectionNumber}>02</span><p>Inside the roadmap</p><h2>{phase.name}<span> / {phase.glyph}*</span></h2><p className={styles.detailDescription}>{phase.description}</p><Link href={`/eips/#fork-${phase.id}`} className={styles.detailLink}>Open full tracker ↗</Link></div>
        <div className={styles.nodeList}><div className={styles.nodeHeader}><span>EF roadmap node</span><span>ETH2030 tracker</span></div>
          {[...groups.entries()].map(([key, group]) => {
            const entry = group[0];
            const nodeContent = <><span className={`${styles.layerLabel} ${layerClasses[entry.layer]}`}>{layerNames[entry.layer]}</span><span className={styles.nodeName}>{entry.strawmap?.item ?? entry.title}<small>{group.length > 1 ? `${group.length} mapped tracker entries` : entry.number ? `EIP-${entry.number}` : 'Roadmap feature'}</small></span><span className={styles.nodeStatus}>{group.length > 1 ? 'View entries' : `Tracker: ${entry.status}`}<small>{group.length === 1 && `${entry.implementation.length ? 'Code referenced' : 'No code listed'} · ${entry.tests.length ? 'tests referenced' : 'no tests listed'}`}</small></span><span className={styles.nodeArrow}>{group.length > 1 ? '+' : '↗'}</span></>;
            return group.length === 1 ? <Link href={`/eips/${entry.id}/`} className={styles.node} key={key}>{nodeContent}</Link> : <details className={styles.nodeGroup} key={key}><summary className={styles.node}>{nodeContent}</summary><div className={styles.groupLinks}>{group.map(item => <Link href={`/eips/${item.id}/`} key={item.id}><span>{item.number ? `EIP-${item.number}` : item.title} <small>{item.title}</small></span><span>Tracker: {item.status} ↗</span></Link>)}</div></details>;
          })}
          {entries.length === 0 && <p className={styles.empty}>No mapped roadmap nodes in the current tracker.</p>}
        </div>
      </section>
      <div className={styles.caveat}><span>About this view</span><p>Roadmap placement follows the EF&apos;s draft drawing, not a confirmed release schedule. The older tracker places leanVM and ethp2p unification in different columns; this view follows the drawing. Tracker labels are historical estimates. “Code referenced” and “tests referenced” describe links; they do not establish completeness, conformance or production readiness. I*–L* are placeholder names. The prototype excludes the drawing&apos;s longer-term column.</p><a href="https://github.com/alt-research/eth2030/blob/master/STATUS.md" target="_blank" rel="noreferrer">Read project status ↗</a></div>
      <footer className={styles.footer}><span>ETH2030 <span className={styles.footerDim}>/ ROADMAP EXPLORER</span></span><a href={source}>Source: EF Architecture ↗</a></footer>
    </div>
  </main>;
}
