import Link from 'next/link';
import type { Metadata } from 'next';
import { GITHUB_URL } from '@/lib/constants';

export const metadata: Metadata = {
  title: 'Terms of Service — ETH2030',
  description: 'Terms of Service for the ETH2030 website and open-source software.',
};

export default function TermsPage() {
  return (
    <main className="min-h-screen bg-eth-bg text-eth-text px-4 py-16">
      <div className="max-w-3xl mx-auto">
        <Link href="/" className="text-sm text-eth-dim hover:text-eth-purple transition-colors mb-8 inline-block">
          &larr; Back to ETH2030
        </Link>

        <h1 className="text-3xl font-bold text-eth-purple mb-2">Terms of Service</h1>
        <p className="text-sm text-eth-dim mb-12">Last updated: November 12, 2026</p>

        <div className="space-y-10 text-sm leading-relaxed text-eth-dim">

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">1. Acceptance of Terms</h2>
            <p>
              By accessing or using the ETH2030 website (<a href="https://eth2030.altllm.ai" className="text-eth-purple hover:underline">eth2030.altllm.ai</a>),
              GitHub repository, documentation, or any associated software (collectively, the &ldquo;Service&rdquo;),
              you agree to be bound by these Terms of Service. If you do not agree, do not use the Service.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">2. Description of Service</h2>
            <p>
              ETH2030 is an <strong className="text-eth-text">experimental, open-source</strong> Ethereum execution client
              targeting the EF L1 Strawmap roadmap. The project is licensed under LGPL-3.0 / GPL-3.0 and is provided
              for research and educational purposes. ETH2030 is not a commercial product and does not offer paid services,
              API access, or hosted infrastructure.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">3. Experimental Software Disclaimer</h2>
            <div className="border border-yellow-500/30 bg-yellow-500/5 rounded-lg p-4 my-4">
              <p className="text-yellow-200/80">
                <strong className="text-yellow-300">ETH2030 is experimental research software.</strong> It is not
                production-ready. It has not been formally audited for security. Do not use it to manage real funds,
                validate mainnet blocks, or run production infrastructure. Running this software on mainnet or any
                network with real value is entirely at your own risk.
              </p>
            </div>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">4. Open-Source License</h2>
            <p>
              The ETH2030 source code is released under the LGPL-3.0 and GPL-3.0 licenses. Your use of the source code
              is governed by those licenses. These Terms of Service govern your use of the website, documentation,
              and any non-code materials provided by the project.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">5. Permitted Use</h2>
            <p>You may use the Service for lawful purposes including:</p>
            <ul className="list-disc list-inside mt-2 space-y-1">
              <li>Reviewing, studying, and learning from the source code</li>
              <li>Running the software on test networks (devnets, testnets)</li>
              <li>Contributing to the project via pull requests</li>
              <li>Forking and modifying the code under the applicable license</li>
              <li>Referencing the project in research or educational materials</li>
            </ul>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">6. Prohibited Uses</h2>
            <p>You agree not to:</p>
            <ul className="list-disc list-inside mt-2 space-y-1">
              <li>Use the software to attack, disrupt, or exploit any blockchain network</li>
              <li>Misrepresent ETH2030 as production-ready or formally audited</li>
              <li>Use the project name or branding to imply endorsement of unrelated products</li>
              <li>Introduce malicious code or vulnerabilities into contributions</li>
              <li>Violate any applicable laws or regulations in your jurisdiction</li>
            </ul>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">7. No Financial Advice</h2>
            <p>
              Nothing on this website or in the ETH2030 documentation constitutes financial, investment, or trading
              advice. The project implements Ethereum protocol specifications for research purposes. Any references to
              tokens, gas, staking, or economic mechanisms are purely technical in nature.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">8. Contributions</h2>
            <p>
              By submitting a pull request or other contribution to ETH2030, you agree that your contribution is
              licensed under the same LGPL-3.0 / GPL-3.0 license as the project. You represent that you have the
              right to make the contribution and that it does not infringe any third-party rights.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">9. Disclaimer of Warranties</h2>
            <p className="uppercase font-semibold text-eth-text/80">
              The Service is provided &ldquo;as is&rdquo; and &ldquo;as available&rdquo; without warranties of any kind,
              either express or implied, including but not limited to warranties of merchantability, fitness for a
              particular purpose, non-infringement, or availability. We do not warrant that the software is free of
              bugs, errors, vulnerabilities, or interruptions.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">10. Limitation of Liability</h2>
            <p className="uppercase font-semibold text-eth-text/80">
              In no event shall the ETH2030 contributors, maintainers, or affiliates be liable for any indirect,
              incidental, special, consequential, or punitive damages, including but not limited to loss of funds,
              data, profits, or goodwill, arising out of or in connection with your use of or inability to use the
              Service, even if advised of the possibility of such damages.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">11. Indemnification</h2>
            <p>
              You agree to indemnify, defend, and hold harmless the ETH2030 contributors and maintainers from any
              claims, damages, losses, or expenses (including reasonable legal fees) arising from your use of the
              Service or your violation of these Terms.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">12. Changes to Terms</h2>
            <p>
              We may update these Terms at any time. Changes take effect when posted on this page. Your continued
              use of the Service after changes constitutes acceptance of the updated Terms. We encourage you to
              review this page periodically.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">13. Governing Law</h2>
            <p>
              These Terms shall be governed by and construed in accordance with applicable law, without regard
              to conflict of law principles.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">14. Contact</h2>
            <p>
              For questions about these Terms, open an issue on our{' '}
              <a href={`${GITHUB_URL}/issues`} target="_blank" rel="noopener noreferrer"
                 className="text-eth-purple hover:underline">GitHub repository</a>.
            </p>
          </section>

        </div>

        <div className="mt-16 pt-6 border-t border-eth-purple/10 text-center text-xs text-eth-dim">
          <Link href="/" className="hover:text-eth-purple transition-colors">ETH2030</Link>
          {' · '}
          <Link href="/privacy" className="hover:text-eth-purple transition-colors">Privacy Policy</Link>
        </div>
      </div>
    </main>
  );
}
