import { readFileSync, existsSync } from 'node:fs';
import { strict as assert } from 'node:assert';

const home = readFileSync(new URL('../out/index.html', import.meta.url), 'utf8');
const pq = readFileSync(new URL('../out/pq-roadmap/index.html', import.meta.url), 'utf8');
const tracker = readFileSync(new URL('../out/eips/index.html', import.meta.url), 'utf8');
assert.match(home, /November 2, 2026/);
assert.match(home, /VERIFICATION INCOMPLETE/i);
assert.match(home, /not production-ready/i);
assert.match(pq, /stub rather than a real signature check/);
assert.doesNotMatch(home + pq, /65\/65|26\/26 gaps|100% EF State Tests|Live on Sepolia &amp; Mainnet/);
assert.match(home, /href="\/eips\/"[^>]*>EIP Progress Tracker/);
for (const fork of ['glamsterdam', 'hegota', 'i', 'j', 'k', 'l', 'longer']) {
  assert.ok(home.includes(`href="/eips/#fork-${fork}"`), `Missing homepage link for ${fork}`);
  assert.ok(tracker.includes(`id="fork-${fork}"`), `Missing tracker section for ${fork}`);
}
for (const path of ['pkg/bal/spec_test.go', 'pkg/epbs/validation_test.go', 'pkg/focil/il_satisfaction_test.go', 'pkg/core/eips/frame_execution_test.go', 'pkg/rollup/execute_test.go', 'pkg/crypto/pqc/pubkey_registry.go']) {
  assert.ok(existsSync(new URL(`../../${path}`, import.meta.url)), `Missing evidence: ${path}`);
}
console.log('Export claims, evidence paths and tracker links verified.');
