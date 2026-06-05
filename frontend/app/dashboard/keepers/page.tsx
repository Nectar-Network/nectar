import { fetchPerformance } from "../../../lib/api";
import Nav from "../../components/Nav";
import KeeperLeaderboard from "./KeeperLeaderboard";

export const dynamic = "force-dynamic";

export default async function KeepersPage() {
  const data = await fetchPerformance();
  return (
    <>
      <Nav />
      <main style={{ paddingTop: "80px", minHeight: "100vh" }}>
        <KeeperLeaderboard initialData={data} />
      </main>
    </>
  );
}
