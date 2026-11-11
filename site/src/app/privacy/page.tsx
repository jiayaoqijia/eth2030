import Link from 'next/link';
import type { Metadata } from 'next';

export const metadata: Metadata = {
  title: 'Privacy Policy — ETH2030',
  description: 'Privacy Policy for the ETH2030 website.',
};

export default function PrivacyPage() {
  return (
    <main className="min-h-screen bg-eth-bg text-eth-text px-4 py-16">
      <div className="max-w-3xl mx-auto">
        <Link href="/" className="text-sm text-eth-dim hover:text-eth-purple transition-colors mb-8 inline-block">
          &larr; Back to ETH2030
        </Link>

        <h1 className="text-3xl font-bold text-eth-purple mb-2">Privacy Policy</h1>
        <p className="text-sm text-eth-dim mb-12">Last updated: March 20, 2026</p>

        <div className="space-y-10 text-sm leading-relaxed text-eth-dim">

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">1. Overview</h2>
            <p>
              This Privacy Policy describes how the ETH2030 project (&ldquo;we&rdquo;, &ldquo;us&rdquo;) handles
              information when you visit our website (<a href="https://eth2030.com" className="text-eth-purple hover:underline">eth2030.com</a>)
              or interact with our open-source software. We are committed to minimizing data collection and
              respecting your privacy.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">2. Information We Collect</h2>

            <h3 className="text-base font-semibold text-eth-text mt-4 mb-2">2.1 Website Analytics</h3>
            <p>
              We use Google Analytics to understand how visitors interact with our website. This service may collect:
            </p>
            <ul className="list-disc list-inside mt-2 space-y-1">
              <li>Pages visited and time spent on each page</li>
              <li>Referral source (how you found our site)</li>
              <li>General geographic region (country/city level, not precise location)</li>
              <li>Device type, browser, and operating system</li>
              <li>Anonymized IP address</li>
            </ul>
            <p className="mt-2">
              Google Analytics uses cookies. You can opt out by using a browser extension such as{' '}
              <a href="https://tools.google.com/dlpage/gaoptout" target="_blank" rel="noopener noreferrer"
                 className="text-eth-purple hover:underline">Google Analytics Opt-out</a>{' '}
              or by configuring your browser to block cookies.
            </p>

            <h3 className="text-base font-semibold text-eth-text mt-4 mb-2">2.2 GitHub Interactions</h3>
            <p>
              When you interact with our GitHub repository (issues, pull requests, discussions), your interactions
              are governed by{' '}
              <a href="https://docs.github.com/en/site-policy/privacy-policies/github-general-privacy-statement"
                 target="_blank" rel="noopener noreferrer" className="text-eth-purple hover:underline">
                GitHub&apos;s Privacy Statement
              </a>. We do not separately collect or store data from GitHub.
            </p>

            <h3 className="text-base font-semibold text-eth-text mt-4 mb-2">2.3 Software Usage</h3>
            <p>
              The ETH2030 software itself does not collect telemetry, usage data, or analytics. It does not phone
              home. When running the software, the only network connections made are those inherent to Ethereum
              P2P networking (peer discovery, block propagation, etc.) which you initiate by running a node.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">3. Information We Do Not Collect</h2>
            <p>We want to be explicit about what we do not collect:</p>
            <ul className="list-disc list-inside mt-2 space-y-1">
              <li>We do not collect personal information (names, emails, phone numbers) through the website</li>
              <li>We do not collect wallet addresses, private keys, or financial information</li>
              <li>We do not require account registration to use the website or software</li>
              <li>We do not track individual users across sessions</li>
              <li>We do not sell, rent, or share personal information with third parties</li>
            </ul>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">4. How We Use Information</h2>
            <p>The limited analytics data we collect through Google Analytics is used solely to:</p>
            <ul className="list-disc list-inside mt-2 space-y-1">
              <li>Understand which documentation pages are most visited</li>
              <li>Improve website content and navigation</li>
              <li>Monitor website performance and availability</li>
            </ul>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">5. Cookies</h2>
            <p>
              Our website uses cookies only through Google Analytics. These are third-party cookies set by Google.
              We do not set any first-party cookies. You can control cookies through your browser settings or opt
              out of Google Analytics entirely.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">6. Third-Party Services</h2>
            <p>Our website and project interact with the following third-party services:</p>
            <table className="w-full mt-3 text-xs">
              <thead>
                <tr className="border-b border-eth-purple/10">
                  <th className="text-left py-2 pr-4 text-eth-text">Service</th>
                  <th className="text-left py-2 pr-4 text-eth-text">Purpose</th>
                  <th className="text-left py-2 text-eth-text">Data Shared</th>
                </tr>
              </thead>
              <tbody className="text-eth-dim">
                <tr className="border-b border-eth-purple/5">
                  <td className="py-2 pr-4">Google Analytics</td>
                  <td className="py-2 pr-4">Website analytics</td>
                  <td className="py-2">Anonymized page views, device info</td>
                </tr>
                <tr className="border-b border-eth-purple/5">
                  <td className="py-2 pr-4">GitHub</td>
                  <td className="py-2 pr-4">Source code hosting</td>
                  <td className="py-2">Per GitHub&apos;s privacy policy</td>
                </tr>
                <tr className="border-b border-eth-purple/5">
                  <td className="py-2 pr-4">GitHub Pages</td>
                  <td className="py-2 pr-4">Website hosting</td>
                  <td className="py-2">Server access logs (IP, user agent)</td>
                </tr>
              </tbody>
            </table>
            <p className="mt-3">
              Each third-party service has its own privacy policy. We recommend reviewing them if you have concerns.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">7. Data Retention</h2>
            <p>
              Google Analytics data is retained according to Google&apos;s default retention settings (typically 14 months).
              We do not maintain any separate databases of user information. Server access logs from our hosting
              provider (GitHub Pages) are retained per their standard policy.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">8. Your Rights</h2>
            <p>Depending on your jurisdiction, you may have the right to:</p>
            <ul className="list-disc list-inside mt-2 space-y-1">
              <li>Opt out of analytics tracking (via browser settings or opt-out extensions)</li>
              <li>Request information about data collected about you</li>
              <li>Request deletion of any data associated with you</li>
            </ul>
            <p className="mt-2">
              Since we collect minimal data and do not maintain user accounts, the most effective privacy control
              is blocking Google Analytics cookies in your browser.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">9. Children&apos;s Privacy</h2>
            <p>
              Our website and software are not directed at individuals under 18. We do not knowingly collect
              information from minors.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">10. International Users</h2>
            <p>
              Our website is hosted globally via GitHub Pages. Analytics data may be processed in the United States
              by Google. By using our website, you acknowledge that your data may be transferred to and processed in
              countries outside your own.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">11. Changes to This Policy</h2>
            <p>
              We may update this Privacy Policy from time to time. Changes take effect when posted on this page.
              We encourage you to review this page periodically. The &ldquo;Last updated&rdquo; date at the top
              indicates the most recent revision.
            </p>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-eth-text mb-3">12. Contact</h2>
            <p>
              For privacy-related questions, open an issue on our{' '}
              <a href="https://github.com/jiayaoqijia/eth2030/issues" target="_blank" rel="noopener noreferrer"
                 className="text-eth-purple hover:underline">GitHub repository</a>.
            </p>
          </section>

        </div>

        <div className="mt-16 pt-6 border-t border-eth-purple/10 text-center text-xs text-eth-dim">
          <Link href="/" className="hover:text-eth-purple transition-colors">ETH2030</Link>
          {' · '}
          <Link href="/terms" className="hover:text-eth-purple transition-colors">Terms of Service</Link>
        </div>
      </div>
    </main>
  );
}
