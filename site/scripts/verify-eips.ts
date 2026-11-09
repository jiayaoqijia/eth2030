#!/usr/bin/env node
// Cross-check src/data/eips.ts against the eth2030 repo docs and codebase.
//
// Usage (from site/):
//   node scripts/verify-eips.ts            # report only
//   node scripts/verify-eips.ts --files    # also verify impl/test file existence under pkg/
//
// Sources checked:
//   1. <repo>/docs/EIP_SPEC_IMPL.md      — per-EIP traceability rows (impl/test paths)
//   2. <repo>/CLAUDE.md                  — Complete/Substantial EIP lists
//   3. <repo>/refs/EIPs/eip-XXXX.md      — spec status in front matter (informational)
//   4. <repo>/pkg/                        — file existence for data entries (--files)

import { readFileSync, existsSync, readdirSync } from 'node:fs';
import { join, dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const siteDir = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const repoDir = resolve(siteDir, '..'); // eth2030 repo root

const { EIPS } = await import(join(siteDir, 'src/data/eips.ts'));

const problems: string[] = [];
const notes: string[] = [];

function read(p: string): string {
  try {
    return readFileSync(join(repoDir, p), 'utf-8');
  } catch {
    return '';
  }
}

// ── 1. EIP_SPEC_IMPL.md ────────────────────────────────────────────────
const specImpl = read('docs/EIP_SPEC_IMPL.md');
const docEips = new Map<number, { impl: string; tests: string; title: string; status?: string }>();
const DOC_STATUS: Record<string, string> = {
  Complete: 'complete',
  Substantial: 'substantial',
  Referenced: 'referenced',
  Partial: 'partial',
  Functional: 'functional',
  'Real impl': 'complete',
  Stub: 'partial',
};
if (specImpl) {
  for (const line of specImpl.split('\n')) {
    const m = line.match(/^\|\s*(\d{3,4})\s*\|\s*([^|]+)\|\s*([^|]+)\|\s*([^|]*)\|\s*([^|]*)\|/);
    if (m) {
      docEips.set(Number(m[1]), {
        title: m[2].trim(),
        status: DOC_STATUS[m[3].trim()],
        impl: m[4].trim(),
        tests: m[5].trim(),
      });
    }
  }
} else {
  notes.push('docs/EIP_SPEC_IMPL.md not readable — skipped doc matrix check');
}

for (const [num, row] of docEips) {
  const entry = EIPS.find((e) => e.number === num);
  if (!entry) {
    problems.push(`EIP-${num} (${row.title}) is in EIP_SPEC_IMPL.md but missing from src/data/eips.ts`);
    continue;
  }
  if (row.impl && !entry.implementation.length) {
    problems.push(`EIP-${num}: doc lists implementation files but data entry has none`);
  }
  if (row.status && entry.status !== row.status) {
    problems.push(`EIP-${num}: doc matrix status is "${row.status}" but data status is ${entry.status}`);
  }
}

for (const e of EIPS) {
  if (e.number !== null && !docEips.has(e.number) && e.fork !== 'longer' && e.category !== 'PQC Suite') {
    notes.push(`EIP-${e.number} in data but not in docs/EIP_SPEC_IMPL.md matrix (OK if newer)`);
  }
}

// ── 2. CLAUDE.md Complete/Substantial lists ────────────────────────────
const claude = read('CLAUDE.md');
if (claude) {
  const completeLine = claude.match(/\*\*Complete\*\*\s*\(\d+\)[::]\s*(.+)/);
  const substantialLine = claude.match(/\*\*Substantial\*\*\s*\(\d+\)[::]\s*(.+)/);
  if (!completeLine || !substantialLine) {
    notes.push('CLAUDE.md Complete/Substantial lists not parsed — check heading format');
  }
  // Strip parentheticals first: descriptions like "EIP-7805 (FOCIL, incl. ... + EIP-8141 ...)"
  // mention other EIP numbers that are not part of the list itself.
  const parseList = (s?: string) =>
    (s?.replace(/\([^)]*\)/g, '').match(/EIP-(\d+)/g) ?? []).map((x) => Number(x.replace('EIP-', '')));
  const complete = parseList(completeLine?.[1]);
  const substantial = parseList(substantialLine?.[1]);
  for (const n of complete) {
    const e = EIPS.find((x) => x.number === n);
    if (e && e.status !== 'complete') {
      problems.push(`EIP-${n}: CLAUDE.md says Complete but data status is ${e.status}`);
    }
  }
  for (const n of substantial) {
    const e = EIPS.find((x) => x.number === n);
    if (e && e.status !== 'substantial') {
      problems.push(`EIP-${n}: CLAUDE.md says Substantial but data status is ${e.status}`);
    }
  }
} else {
  notes.push('CLAUDE.md not readable — skipped status-list check');
}

// ── 3. refs/EIPs spec status (informational) ───────────────────────────
const refsDir = join(repoDir, 'refs/EIPs/EIPS');
if (existsSync(refsDir)) {
  for (const e of EIPS) {
    if (e.number === null) continue;
    const f = join(refsDir, `eip-${e.number}.md`);
    if (!existsSync(f)) {
      notes.push(`EIP-${e.number}: no spec file in refs/EIPs (may live in EIPS/eip-XXXX.md naming variant)`);
      continue;
    }
    const head = readFileSync(f, 'utf-8').slice(0, 600);
    const m = head.match(/status[::]\s*(\w+)/i);
    if (m && e.specStatus && !e.specStatus.toLowerCase().includes(m[1].toLowerCase())) {
      notes.push(`EIP-${e.number}: refs/EIPs status=${m[1]}, data specStatus=${e.specStatus}`);
    }
    if (m && !e.specStatus && e.status !== 'planned' && e.fork !== 'pre') {
      // only note upstream status for recent EIPs
      if (['glamsterdam', 'hegota', 'i', 'j'].includes(e.fork)) {
        notes.push(`EIP-${e.number}: upstream spec status is "${m[1]}" — consider setting specStatus`);
      }
    }
  }
}

// ── 4. File existence under pkg/ ───────────────────────────────────────
let checked = 0;
let missing = 0;
if (process.argv.includes('--files')) {
  const pkgDir = join(repoDir, 'pkg');
  const expand = (p: string): string[] => {
    // supports a single * wildcard segment like bal/*_test.go
    const norm = p.replace(/^pkg\//, '');
    const full = join(pkgDir, norm);
    if (!norm.includes('*')) return existsSync(full) ? [norm] : [];
    const dir = join(pkgDir, dirname(norm));
    if (!existsSync(dir)) return [];
    const rx = new RegExp('^' + norm.split('/').pop()!.replace(/\*/g, '.*') + '$');
    return readdirSync(dir)
      .filter((f) => rx.test(f))
      .map((f) => join(dirname(norm), f));
  };
  for (const e of EIPS) {
    for (const p of e.implementation) {
      for (const part of p.split(',').map((s) => s.trim())) {
        if (part.endsWith('/')) {
          if (!existsSync(join(pkgDir, part.replace(/\/$/, '')))) {
            missing++;
            problems.push(`${e.id}: implementation dir pkg/${part} does not exist`);
          }
          checked++;
          continue;
        }
        const hits = expand(part);
        checked++;
        if (hits.length === 0) {
          missing++;
          problems.push(`${e.id}: implementation file pkg/${part} not found`);
        }
      }
    }
    for (const p of e.tests) {
      const hits = expand(p);
      checked++;
      if (hits.length === 0) {
        missing++;
        problems.push(`${e.id}: test file pkg/${p} not found`);
      }
    }
  }
}

// ── Report ─────────────────────────────────────────────────────────────
console.log(`eth2030 EIP tracker verify — ${EIPS.length} entries in src/data/eips.ts`);
console.log(`docs/EIP_SPEC_IMPL.md matrix rows parsed: ${docEips.size}`);
if (process.argv.includes('--files')) {
  console.log(`pkg/ paths checked: ${checked}, missing: ${missing}`);
}
if (problems.length) {
  console.log(`\n✗ ${problems.length} problem(s):`);
  for (const p of problems) console.log('  ✗ ' + p);
}
if (notes.length) {
  console.log(`\nℹ ${notes.length} note(s):`);
  for (const n of notes.slice(0, 40)) console.log('  ℹ ' + n);
  if (notes.length > 40) console.log(`  … and ${notes.length - 40} more`);
}
if (!problems.length) console.log('\n✓ no blocking discrepancies');
process.exit(problems.length ? 1 : 0);
