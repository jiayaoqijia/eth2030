'use client';
import Link from 'next/link';
import GlitchText from '@/components/ui/GlitchText';
import MatrixRain from '@/components/ui/MatrixRain';
import { motion } from 'framer-motion';
import { GITHUB_URL, DOCS_URL } from '@/lib/constants';
import styles from './HeroActions.module.css';

export default function Hero() {
  return (
    <section className="relative min-h-screen flex flex-col items-center justify-center px-4 py-24 overflow-hidden">
      <MatrixRain />

      <div
        className="absolute inset-0 bg-grid-pattern opacity-50"
        aria-hidden="true"
      />

      <div
        className="absolute inset-0"
        style={{
          background: 'radial-gradient(ellipse at center, rgba(58, 28, 113, 0.2), transparent 70%)',
        }}
        aria-hidden="true"
      />

      <div className="relative z-10 text-center max-w-4xl mx-auto">
        <motion.div
          initial={{ opacity: 0, y: 20 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.6 }}
          className="inline-block mb-6 px-4 py-1.5 rounded-full border border-eth-purple/30
                     bg-eth-purple/5 text-eth-purple text-sm tracking-wider uppercase"
        >
          Ethereum Execution Client
        </motion.div>

        <motion.div
          initial={{ opacity: 0, scale: 0.95 }}
          animate={{ opacity: 1, scale: 1 }}
          transition={{ duration: 0.8, delay: 0.2 }}
        >
          <GlitchText
            text="ETH2030"
            className="text-6xl sm:text-7xl md:text-8xl lg:text-9xl font-bold tracking-tighter"
          />
        </motion.div>

        <motion.p
          initial={{ opacity: 0, y: 20 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.6, delay: 0.5 }}
          className="mt-6 text-lg sm:text-xl md:text-2xl text-eth-dim max-w-2xl mx-auto leading-relaxed"
        >
          An experimental Ethereum execution client in Go, exploring the{' '}
          <a href="https://strawmap.org/" target="_blank" rel="noopener noreferrer" className="underline decoration-eth-purple/50 hover:decoration-eth-purple">L1 Strawmap</a>{' '}
          through open code, tests and protocol research.
        </motion.p>

        <motion.div
          initial={{ opacity: 0, y: 20 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.6, delay: 0.8 }}
          className={styles.actions}
        >
          <Link
            href="/roadmap/"
            className={`${styles.action} ${styles.roadmap}`}
            data-hero-action="roadmap"
          >
            <span className={styles.icon} aria-hidden="true"><svg viewBox="0 0 24 24" fill="none"><path d="M4 17.5V6.5L9 4l6 3 5-2.5v11L15 18l-6-3-5 2.5Zm5-13.2v10.4m6-7.4v10.4" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" /></svg></span>
            <span className={styles.label}><strong>Roadmap</strong><small>Explore our progress</small></span>
            <span className={styles.arrow} aria-hidden="true">→</span>
          </Link>
          <a
            href={GITHUB_URL}
            target="_blank"
            rel="noopener noreferrer"
            className={`${styles.action} ${styles.github}`}
            data-hero-action="github"
          >
            <span className={styles.icon} aria-hidden="true"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 2a10 10 0 0 0-3.16 19.49c.5.09.68-.21.68-.48v-1.7c-2.78.61-3.37-1.18-3.37-1.18-.45-1.14-1.1-1.45-1.1-1.45-.9-.62.07-.61.07-.61 1 .07 1.53 1.03 1.53 1.03.89 1.52 2.34 1.08 2.91.82.09-.64.35-1.08.63-1.33-2.22-.25-4.56-1.11-4.56-4.95 0-1.09.39-1.98 1.03-2.68-.11-.25-.45-1.27.1-2.65 0 0 .84-.27 2.75 1.02A9.58 9.58 0 0 1 12 6.99c.85 0 1.71.12 2.51.34 1.91-1.29 2.75-1.02 2.75-1.02.55 1.38.21 2.4.1 2.65.64.7 1.03 1.59 1.03 2.68 0 3.85-2.35 4.69-4.58 4.94.36.31.68.92.68 1.86v2.57c0 .27.18.58.69.48A10 10 0 0 0 12 2Z" /></svg></span>
            <span className={styles.label}><strong>GitHub</strong><small>Browse the source</small></span>
            <span className={styles.arrow} aria-hidden="true">↗</span>
          </a>
          <a
            href={DOCS_URL}
            target="_blank"
            rel="noopener noreferrer"
            className={`${styles.action} ${styles.docs}`}
            data-hero-action="docs"
          >
            <span className={styles.icon} aria-hidden="true"><svg viewBox="0 0 24 24" fill="none"><path d="M5 4.75h9.5L19 9.25v10H5v-14.5Zm9.5 0v4.5H19M8 13h8m-8 3h8" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" strokeLinecap="round" /></svg></span>
            <span className={styles.label}><strong>Docs</strong><small>Build and run</small></span>
            <span className={styles.arrow} aria-hidden="true">↗</span>
          </a>
        </motion.div>

        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.6, delay: 1.1 }}
          className="mt-8 text-eth-dim text-sm"
        >
          Experimental research software · not production-ready · Go 1.26.8+
        </motion.div>
      </div>

      <motion.div
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        transition={{ delay: 1.5 }}
        className="absolute bottom-8 left-1/2 -translate-x-1/2"
      >
        <motion.div
          animate={{ y: [0, 8, 0] }}
          transition={{ duration: 1.5, repeat: Infinity }}
          className="w-6 h-10 rounded-full border-2 border-eth-purple/30 flex items-start justify-center p-1.5"
        >
          <div className="w-1.5 h-1.5 rounded-full bg-eth-purple" />
        </motion.div>
      </motion.div>
    </section>
  );
}
