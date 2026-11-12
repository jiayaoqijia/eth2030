'use client';
import Link from 'next/link';
import { GITHUB_URL, DOCS_URL } from '@/lib/constants';

export default function Footer() {
  return (
    <footer className="border-t border-eth-purple/10 py-12 px-4">
      <div className="max-w-6xl mx-auto">
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-8">
          <div>
            <div className="flex items-center gap-2 mb-2">
              <img src="/logo.svg" alt="ETH2030" className="w-6 h-6" />
              <h3 className="text-lg font-bold text-eth-purple">ETH2030</h3>
            </div>
            <p className="text-sm text-eth-dim leading-relaxed">
              Experimental Ethereum execution client targeting the{' '}
              <a href="https://strawmap.org/" target="_blank" rel="noopener noreferrer"
                 className="text-eth-purple hover:underline">L1 Strawmap</a>.
            </p>
          </div>

          <div>
            <h4 className="text-sm font-semibold text-eth-text uppercase tracking-wider mb-3">Links</h4>
            <ul className="space-y-2">
              <li>
                <Link href="/roadmap/" className="text-sm text-eth-dim hover:text-eth-purple transition-colors">
                  Roadmap
                </Link>
              </li>
              <li>
                <a href={GITHUB_URL} target="_blank" rel="noopener noreferrer"
                   className="text-sm text-eth-dim hover:text-eth-purple transition-colors">
                  GitHub
                </a>
              </li>
              <li>
                <a href={DOCS_URL} target="_blank" rel="noopener noreferrer"
                   className="text-sm text-eth-dim hover:text-eth-purple transition-colors">
                  Documentation
                </a>
              </li>
              <li>
                <a href={`${GITHUB_URL}/issues`} target="_blank" rel="noopener noreferrer"
                   className="text-sm text-eth-dim hover:text-eth-purple transition-colors">
                  Issues
                </a>
              </li>
            </ul>
          </div>

          <div>
            <h4 className="text-sm font-semibold text-eth-text uppercase tracking-wider mb-3">Legal</h4>
            <ul className="space-y-2">
              <li>
                <Link href="/terms" className="text-sm text-eth-dim hover:text-eth-purple transition-colors">
                  Terms of Service
                </Link>
              </li>
              <li>
                <Link href="/privacy" className="text-sm text-eth-dim hover:text-eth-purple transition-colors">
                  Privacy Policy
                </Link>
              </li>
              <li className="text-sm text-eth-dim">LGPL-3.0 / GPL-3.0</li>
            </ul>
          </div>
        </div>

        <div className="mt-10 pt-6 border-t border-eth-purple/5 text-center text-xs text-eth-dim">
          ETH2030 is experimental research software. Not production-ready. Use at your own risk.
        </div>
      </div>
    </footer>
  );
}
