import Nav from "./components/Nav";
import HomeHero from "./components/HomeHero";
import ProblemStats from "./components/ProblemStats";
import Architecture from "./components/Architecture";
import NetworkNow from "./components/NetworkNow";
import MonitorFeed from "./components/MonitorFeed";
import VaultCTA from "./components/VaultCTA";
import Footer from "./components/Footer";

export default function Page() {
  return (
    <main className="min-h-screen">
      <Nav />
      <HomeHero />
      <ProblemStats />
      <Architecture />
      <NetworkNow />
      <MonitorFeed />
      <VaultCTA />
      <Footer />
    </main>
  );
}
