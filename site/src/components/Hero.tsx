'use client';
import GlitchText from '@/components/ui/GlitchText';
import MatrixRain from '@/components/ui/MatrixRain';
import { motion } from 'framer-motion';
import { GITHUB_URL, DOCS_URL } from '@/lib/constants';

export default function Hero() {
  return (
    <section className="relative min-h-screen flex flex-col items-center justify-center px-4 overflow-hidden">
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
          className="mt-10 flex flex-col sm:flex-row gap-4 justify-center"
        >
          <a
            href={GITHUB_URL}
            target="_blank"
            rel="noopener noreferrer"
            className="px-8 py-3 rounded-lg bg-eth-purple/10 border border-eth-purple/50
                       text-eth-purple font-semibold hover:bg-eth-purple/20
                       hover:shadow-[0_0_20px_#8c8dfc33] transition-all duration-300
                       text-center"
          >
            View on GitHub
          </a>
          <a
            href={DOCS_URL}
            target="_blank"
            rel="noopener noreferrer"
            className="px-8 py-3 rounded-lg bg-eth-pink/10 border border-eth-pink/50
                       text-eth-pink font-semibold hover:bg-eth-pink/20
                       hover:shadow-[0_0_20px_#ff6b9d33] transition-all duration-300
                       text-center"
          >
            Documentation
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
