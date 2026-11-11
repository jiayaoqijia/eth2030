import Link from 'next/link';

const repo = 'https://github.com/jiayaoqijia/eth2030';
const revision = '14497b5fefa08cdba038f92bd2c29f82ad09b625';
const base = `${repo}/blob/${revision}`;
const tree = `${repo}/tree/${revision}`;

export default function PQRoadmapPage() {
  return <main className="min-h-screen px-4 py-16 md:py-24"><div className="max-w-4xl mx-auto">
    <Link href="/" className="text-eth-teal underline text-sm">← Back to ETH2030</Link>
    <p className="mt-12 text-eth-teal text-xs uppercase tracking-widest">Research notes · November 5, 2026</p>
    <h1 className="text-4xl md:text-6xl font-bold mt-4 text-eth-text">Post-quantum Ethereum</h1>
    <p className="mt-6 text-eth-text/80 leading-8">The EF Strawmap explores a hash-based post-quantum L1 across consensus, data availability and execution. ETH2030 contains related experimental code; the mapping below does not establish compatibility with the evolving designs.</p>
    <div className="mt-10 p-6 rounded-xl border border-eth-pink/30 bg-eth-pink/5 text-sm text-eth-text/90 leading-7">The November 2 Strawmap is a draft. The EF&apos;s September description of minimum viable post-quantum security is historical context; consult the current map for milestone placement. No claim of quantum resistance, audited cryptography or production readiness is made here.</div>
    <h2 className="text-2xl font-bold mt-16 mb-6 text-eth-purple">Directions and evidence</h2>
    <div className="space-y-4">
      <article className="p-6 rounded-xl border border-eth-purple/25 bg-eth-surface/50"><h3 className="text-lg font-bold text-eth-text">PQ key registration</h3><p className="text-sm leading-7 mt-3 text-eth-text/80">A validator key registry and unit tests exist. Its registration verification explicitly uses a stub rather than a real signature check; it is not a secure registration implementation.</p><p className="mt-4 text-sm"><a className="text-eth-teal underline" href={`${base}/pkg/crypto/pqc/pubkey_registry.go`}>Source</a> · <a className="text-eth-teal underline" href={`${base}/pkg/crypto/pqc/pubkey_registry_test.go`}>Tests</a></p></article>
      <article className="p-6 rounded-xl border border-eth-purple/25 bg-eth-surface/50"><h3 className="text-lg font-bold text-eth-text">Signatures and attestations</h3><p className="text-sm leading-7 mt-3 text-eth-text/80">The repository contains hash-signature and PQ attestation experiments. Their presence does not establish conformance to leanSPHINCS or leanXMSS, security review, or integration with a production consensus client.</p><p className="mt-4 text-sm"><a className="text-eth-teal underline" href={`${tree}/pkg/crypto/pqc`}>Cryptography code</a> · <a className="text-eth-teal underline" href={`${tree}/pkg/consensus/pq`}>Attestation code and tests</a></p></article>
      <article className="p-6 rounded-xl border border-eth-purple/25 bg-eth-surface/50"><h3 className="text-lg font-bold text-eth-text">Proofs and data availability</h3><p className="text-sm leading-7 mt-3 text-eth-text/80">Proof and data-availability experiments need a separate comparison with leanVM and leanDA specifications and independent interoperability and security testing. No equivalence is asserted.</p><p className="mt-4 text-sm"><a className="text-eth-teal underline" href={`${tree}/pkg/proofs`}>Proof code</a> · <a className="text-eth-teal underline" href={`${tree}/pkg/das`}>Data availability code</a></p></article>
    </div>
    <h2 className="text-2xl font-bold mt-16 mb-4 text-eth-purple">Primary sources</h2>
    <ul className="space-y-3 text-sm leading-6"><li><a className="text-eth-teal underline" href="https://strawmap.org/">EF Architecture Strawmap and current drawing</a></li><li><a className="text-eth-teal underline" href="https://blog.ethereum.org/2026/09/07/protocol-priorities">EF Protocol: Current and Emerging Priorities, September 2026</a></li><li><a className="text-eth-teal underline" href="https://eips.ethereum.org/EIPS/eip-8141">EIP-8141: Frame Transaction</a></li></ul>
  </div></main>;
}
