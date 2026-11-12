import { EIPS, type EipEntry, type ForkId, type Layer, type Status } from '@/data/eips';

export const roadmapStages: { id: ForkId; name: string; glyph: string; heading: string; description: string }[] = [
  { id: 'glamsterdam', name: 'Glamsterdam', glyph: 'G*', heading: 'Scale the execution layer', description: 'Block access lists, ePBS and the foundations for faster L1 execution.' },
  { id: 'hegota', name: 'Hegotá', glyph: 'H*', heading: 'Open the protocol', description: 'Inclusion lists, quick slots and programmable transactions.' },
  { id: 'i', name: 'I*', glyph: 'I*', heading: 'The next foundations', description: 'Decoupled consensus, binary state and post-quantum research.' },
  { id: 'j', name: 'J*', glyph: 'J*', heading: 'Towards post-quantum', description: 'Attestations, sampling and proofs for a new security model.' },
  { id: 'k', name: 'K*', glyph: 'K*', heading: 'Proofs and state', description: 'Mandatory proofs and a path towards decentralized state.' },
  { id: 'l', name: 'L*', glyph: 'L*', heading: 'The longer horizon', description: 'One-round finality, native rollups and protocol simplification.' },
];

export const roadmapStatuses: Status[] = ['complete', 'substantial', 'functional', 'partial', 'referenced', 'planned'];
export const roadmapStatusNames: Record<Status, string> = { complete: 'Complete', substantial: 'Substantial', functional: 'Functional', partial: 'Partial', referenced: 'Referenced', planned: 'Planned' };

// These positions follow the EF drawing where the older tracker differs.
export function roadmapFork(entry: EipEntry): ForkId {
  if (entry.id === 'feature-leanvm-pq-proofs') return 'i';
  if (entry.id === 'feature-ethp2p-unification') return 'l';
  return entry.fork;
}

export function roadmapLayer(entry: EipEntry): Layer {
  if (['feature-fast-payload-broadcast', 'feature-ethp2p-unification', 'eip-8077'].includes(entry.id)) return 'EL';
  return entry.layer;
}

export function roadmapProgress(fork: ForkId) {
  const entries = EIPS.filter(entry => roadmapFork(entry) === fork && entry.strawmap);
  const counts = Object.fromEntries(roadmapStatuses.map(status => [status, entries.filter(entry => entry.status === status).length])) as Record<Status, number>;
  const completePercentage = entries.length ? Math.round((counts.complete / entries.length) * 100) : null;
  return { entries, counts, completePercentage };
}

export function groupRoadmapNodes(entries: EipEntry[]) {
  const groups = new Map<string, EipEntry[]>();
  for (const entry of entries) {
    const key = `${roadmapLayer(entry)}:${entry.strawmap?.item}`;
    groups.set(key, [...(groups.get(key) ?? []), entry]);
  }
  return [...groups.values()];
}
