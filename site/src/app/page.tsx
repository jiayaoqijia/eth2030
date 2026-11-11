'use client';
import Hero from '@/components/Hero';
import FeaturesGrid from '@/components/FeaturesGrid';
import RoadmapTimeline from '@/components/RoadmapTimeline';
import GettingStarted from '@/components/GettingStarted';
import Footer from '@/components/Footer';

export default function Home() {
  return (
    <main className="min-h-screen">
      <Hero />
      <FeaturesGrid />
      <RoadmapTimeline />
      <GettingStarted />
      <Footer />
    </main>
  );
}
