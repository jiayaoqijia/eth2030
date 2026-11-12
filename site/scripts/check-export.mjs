import { readFileSync, existsSync, readdirSync } from 'node:fs';
import { strict as assert } from 'node:assert';

const home = readFileSync(new URL('../out/index.html', import.meta.url), 'utf8');
const pq = readFileSync(new URL('../out/pq-roadmap/index.html', import.meta.url), 'utf8');
const tracker = readFileSync(new URL('../out/eips/index.html', import.meta.url), 'utf8');
assert.match(home, /November 2, 2026/);
assert.match(home, /VERIFICATION INCOMPLETE/i);
assert.match(home, /not production-ready/i);
assert.match(home, /github\.com\/alt-research\/eth2030/);
assert.match(home, /Go 1\.26\.8\+/);
assert.match(home, /Moving-pivot range reuse/);
assert.match(pq, /stub rather than a real signature check/);
assert.doesNotMatch(home + pq, /65\/65|26\/26 gaps|100% EF State Tests|Live on Sepolia &amp; Mainnet/);
assert.match(home, /href="\/eips\/"[^>]*>EIP Progress Tracker/);
for (const fork of ['glamsterdam', 'hegota', 'i', 'j', 'k', 'l', 'longer']) {
  assert.ok(home.includes(`href="/eips/#fork-${fork}"`), `Missing homepage link for ${fork}`);
  assert.ok(tracker.includes(`id="fork-${fork}"`), `Missing tracker section for ${fork}`);
}
for (const [id, expected, obsolete] of [
  ['eip-7928', 'BAL header commitment encoded', 'block_access_list_hash header field not yet added'],
  ['eip-7805', 'Inclusion-list gossip validation', 'CL P2P gossip validation rules for ILs'],
  ['eip-8141', 'APPROVE scope handling', 'Payer gas-approval logic'],
  ['eip-7495', 'ssz/stable_container_test.go', 'No tests'],
]) {
  const page = readFileSync(new URL(`../out/eips/${id}/index.html`, import.meta.url), 'utf8');
  assert.ok(page.includes(expected), `Missing current evidence on ${id}`);
  assert.ok(!page.includes(obsolete), `Obsolete claim on ${id}`);
}
for (const path of ['pkg/bal/spec_test.go', 'pkg/epbs/validation_test.go', 'pkg/focil/il_satisfaction_test.go', 'pkg/core/eips/frame_execution_test.go', 'pkg/rollup/execute_test.go', 'pkg/crypto/pqc/pubkey_registry.go']) {
  assert.ok(existsSync(new URL(`../../${path}`, import.meta.url)), `Missing evidence: ${path}`);
}
function checkPages(dir) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = new URL(`${entry.name}${entry.isDirectory() ? '/' : ''}`, dir);
    if (entry.isDirectory()) checkPages(path);
    else if (entry.name.endsWith('.html')) {
      const html = readFileSync(path, 'utf8');
      assert.doesNotMatch(html, /github\.com\/jiayaoqijia\/eth2030|https:\/\/eth2030\.com/, `Outdated link in ${path.pathname}`);
    }
  }
}
checkPages(new URL('../out/', import.meta.url));
const pagesWorkflow = readFileSync(new URL('../../.github/workflows/deploy-pages.yml', import.meta.url), 'utf8');
assert.match(pagesWorkflow, /echo 'eth2030\.altllm\.ai' > out\/CNAME/);
console.log('Export claims, evidence paths and tracker links verified.');
