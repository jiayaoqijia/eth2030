'use client';
import SectionHeading from '@/components/ui/SectionHeading';
import TerminalBlock from '@/components/ui/TerminalBlock';
import { gettingStartedCommands } from '@/lib/data';

export default function GettingStarted() {
  return (
    <section id="getting-started" className="py-20 md:py-28 px-4 bg-eth-surface/30">
      <SectionHeading
        title="Getting Started"
        subtitle="Build and explore the code"
      />

      <div className="max-w-3xl mx-auto">
        <TerminalBlock commands={gettingStartedCommands} />

        <div className="mt-8 text-center text-sm text-eth-dim">
          Requires Go 1.26.8+. Running an Ethereum node also requires a compatible consensus client; see the project documentation for configuration and current limitations.
        </div>
      </div>
    </section>
  );
}
