import type { Metadata } from 'next';
import { JetBrains_Mono } from 'next/font/google';
import Script from 'next/script';
import './globals.css';

const jetbrainsMono = JetBrains_Mono({
  subsets: ['latin'],
  variable: '--font-jetbrains',
  display: 'swap',
});

export const metadata: Metadata = {
  title: 'ETH2030 — Ethereum Client for the L1 Strawmap',
  description:
    'Experimental Ethereum execution client exploring the L1 Strawmap through open code, tests and protocol research.',
  keywords: [
    'Ethereum', 'ETH2030', 'execution client', 'EVM', 'zkVM',
    'PeerDAS', '3SF', 'post-quantum', 'ePBS', 'native rollups',
  ],
  authors: [{ name: 'ETH2030' }],
  icons: {
    icon: '/favicon.ico',
    apple: '/apple-touch-icon.png',
  },
  openGraph: {
    title: 'ETH2030 — Ethereum Client for the L1 Strawmap',
    description:
      'Experimental Ethereum execution client exploring the L1 Strawmap through open code and protocol research.',
    siteName: 'ETH2030',
    images: [{ url: '/og-image.png', width: 1200, height: 630, alt: 'ETH2030' }],
    locale: 'en_US',
    type: 'website',
  },
  twitter: {
    card: 'summary_large_image',
    title: 'ETH2030 — Ethereum Client for the L1 Strawmap',
    description: 'Experimental Ethereum execution client exploring the L1 Strawmap.',
    images: ['/og-image.png'],
  },
  robots: { index: true, follow: true },
  metadataBase: new URL('https://eth2030.altllm.ai'),
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className={jetbrainsMono.variable}>
      <head>
        <Script
          src="https://www.googletagmanager.com/gtag/js?id=G-X7E4QDFTLB"
          strategy="afterInteractive"
        />
        <Script id="gtag-init" strategy="afterInteractive">
          {`
            window.dataLayer = window.dataLayer || [];
            function gtag(){dataLayer.push(arguments);}
            gtag('js', new Date());
            gtag('config', 'G-X7E4QDFTLB');
          `}
        </Script>
      </head>
      <body className="bg-eth-bg text-eth-text antialiased overflow-x-hidden font-mono">
        {children}
        <div className="scanline-overlay" aria-hidden="true" />
      </body>
    </html>
  );
}
