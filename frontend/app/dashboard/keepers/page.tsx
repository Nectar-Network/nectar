import { fetchPerformance } from "../../../lib/api";
import Nav from "../../components/Nav";
import Footer from "../../components/Footer";
import DashSubnav from "../../components/DashSubnav";
import KeeperLeaderboard from "./KeeperLeaderboard";

export const dynamic = "force-dynamic";

export default async function KeepersPage() {
  const data = await fetchPerformance();
  return (
    <>
      <Nav />
      <DashSubnav />
      <main style={{ paddingTop: 64, minHeight: "100vh" }}>
        <KeeperLeaderboard initialData={data} />
      </main>
      <Footer />
    </>
  );
}
