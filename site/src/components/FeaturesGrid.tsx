import SectionHeading from '@/components/ui/SectionHeading';
import { evidence } from '@/lib/data';

const revision = '14497b5fefa08cdba038f92bd2c29f82ad09b625';
const base = `https://github.com/jiayaoqijia/eth2030`;

export default function FeaturesGrid() {
  return <section id="features" className="py-20 md:py-28 px-4">
    <SectionHeading title="Research you can inspect" subtitle="Selected code and tests, with the limits of this review" />
    <div className="max-w-5xl mx-auto">
      <p className="text-eth-text/80 text-sm leading-7 mb-8">ETH2030 is an experimental execution client and research codebase. The entries below identify code to review, not claims of full EIP conformance or production readiness. Evidence checked against repository revision <a className="text-eth-teal underline" href={`https://github.com/jiayaoqijia/eth2030/commit/${revision}`}>14497b5</a> on November 5, 2026.</p>
      <div className="grid md:grid-cols-2 gap-4">
        {evidence.map(item => <article key={item.eip} className="rounded-xl border border-eth-purple/25 bg-eth-surface/60 p-6">
          <p className="text-xs uppercase tracking-wider text-eth-teal mb-2">Code present · verification incomplete</p>
          <h3 className="text-lg font-bold text-eth-text mb-3">{item.title}</h3>
          <p className="text-sm text-eth-text/80 leading-6">{item.summary}</p>
          <p className="text-sm text-eth-dim leading-6 mt-3">{item.limit}</p>
          <div className="flex flex-wrap gap-4 text-sm mt-5"><a className="text-eth-purple underline" href={`https://eips.ethereum.org/EIPS/eip-${item.eip}`}>{item.spec}</a><a className="text-eth-teal underline" href={`${base}/${item.code.endsWith('.go') ? 'blob' : 'tree'}/${revision}/${item.code}`}>Source</a><a className="text-eth-teal underline" href={`${base}/blob/${revision}/${item.test}`}>Tests</a></div>
        </article>)}
      </div>
      <p className="mt-8 text-sm text-eth-dim leading-7">Unit tests exercise specific code paths; they do not establish protocol conformance, security or cross-client interoperability. The roadmap is broader than the areas reviewed here.</p>
    </div>
  </section>;
}
