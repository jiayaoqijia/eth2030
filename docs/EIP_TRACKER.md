# EIP Progress Tracker — Maintenance Guide

The site at `site/` renders a per-EIP progress tracker at **`/eips`** (overview) and
**`/eips/<id>`** (one page per EIP / roadmap feature). This document explains the data
model and the update workflow.

## Data source of truth

`site/src/data/eips.ts` is the **single source of truth**. Everything on the `/eips`
pages is generated from it — edit the data, not the markup, to change progress.

### Fork placement baseline

Fork placement (Glamsterdam → Hegotá → I* → J* → K* → L* → longer term) follows the
**L1 Strawmap revision of Nov 2, 2026** (strawmap.org). Note this differs from the
older derivation in `docs/ROADMAP.md` (Feb 2026), e.g. FOCIL and frame transactions
are now Hegotá headliners, native rollups moved to L*. The strawmap is re-published
roughly quarterly — re-check placement after each revision.

The strawmap visual is a Google Drawings embed; to extract it:

```bash
curl -sL 'https://docs.google.com/drawings/d/1GkcGfQv9kxgrYQMyu0BYGcZya0gIDG_wQ7GsFJEsdzY/export/svg?id=1GkcGfQv9kxgrYQMyu0BYGcZya0gIDG_wQ7GsFJEsdzY' -o /tmp/strawmap.svg
# text is baked into paths — render and read it visually:
google-chrome --headless --screenshot=/tmp/strawmap.png --window-size=2979,2272 file:///tmp/strawmap.svg
```

### Entry schema (`EipEntry`)

| Field | Meaning |
|-------|---------|
| `id` | URL slug: `eip-<n>` or `feature-<slug>` |
| `number` | EIP number; `null` for roadmap features (strawmap boxes without an EIP) |
| `layer` | `CL` / `DL` / `EL` / `cross` |
| `fork` | `pre` / `glamsterdam` / `hegota` / `i` / `j` / `k` / `l` / `longer` / `infra` |
| `status` | `complete` / `substantial` / `functional` / `referenced` / `partial` / `planned` |
| `completeness` | 0–100, only where a gap analysis quantified it (11 key EIPs) |
| `priority` | `P0` / `P1` / `P2` from the gap analysis |
| `specUrl` / `specStatus` | Spec link and upstream status (from `refs/EIPs/EIPS/eip-*.md` front matter) |
| `strawmap` | Box label on the strawmap + `SFI`/`CFI`/`EIP` tag + headliner/offchain/crossLayer flags |
| `implementation` / `tests` | Paths **relative to `pkg/`** (verified to exist by the verify script) |
| `implemented` / `gaps` | Deep-analysis content (the 11 gap-analyzed EIPs) |

Status semantics: `complete` = full implementation per `EIP_SPEC_IMPL.md`;
`substantial` = major parts in place (ePBS, FOCIL, execution proofs, binary trie);
`functional` = framework-level roadmap feature with working code; `referenced` =
spec consulted / partial hooks only; `partial` = known incomplete; `planned` =
strawmap box with no eth2030 implementation yet.

## Update workflow (after code changes)

```bash
cd site
node scripts/verify-eips.ts          # cross-check data vs docs + CLAUDE.md + refs/EIPs
node scripts/verify-eips.ts --files  # additionally verify every pkg/ path exists
npm run build                        # type-check + regenerate static pages
```

1. Implement/modify EIP code in `pkg/` as usual.
2. Run `verify-eips.ts --files` — fix any drift it reports by updating
   `site/src/data/eips.ts` (paths, status, completeness, gaps).
3. `npm run build` and spot-check `/eips/<id>` pages.
4. Commit everything in **this repo only** (site + docs + code together is fine);
   action-oriented commit message, no AI co-author line.

The verify script checks, in order of severity:

- **Problems (exit 1)**: EIP present in `docs/EIP_SPEC_IMPL.md` matrix but missing
  from the data file; missing implementation entries; status conflicts with the
  `Complete`/`Substantial` lists in `CLAUDE.md`; non-existent `pkg/` paths (with `--files`).
- **Notes (informational)**: upstream spec-status mismatches vs `refs/EIPs`, data
  entries not yet in the doc matrix, spec files not found in refs.

## Cross-repo notes

CL-side EIPs (7251, 7549, 7732, 7805, PQ attestations) are also implemented in the
Lighthouse fork — their eth2030 pages only track the EL-repo/`pkg/` side plus the
shared types. Update `lighthouse/CLAUDE.md` and `docs/LIGHTHOUSE_DEVELOPMENT_ROADMAP.md`
separately in that repo.

## Related documents

- `docs/EIP_SPEC_IMPL.md` — per-EIP traceability matrix (doc-side source)
- `docs/GAP_ANALYSIS.md` — 65 roadmap-item audit
- `docs/EIP_IMPLEMENTATION_GAP_ANALYSIS.md` (workspace root `docs/`) — completeness % + P0/P1/P2 for 11 key EIPs
- `docs/ROADMAP.md` — older Feb 2026 strawmap derivation (kept for history; fork
  placement on the site follows the Nov 2026 strawmap instead)
