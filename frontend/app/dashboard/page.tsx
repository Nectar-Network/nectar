import { fetchPerformance } from "../../lib/api";
import Nav from "../components/Nav";
import DashboardOverview from "./DashboardOverview";

export const dynamic = "force-dynamic";

export default async function DashboardPage() {
  const data = await fetchPerformance();
  return (
    <>
      <Nav />
      <main style={{ paddingTop: "80px", minHeight: "100vh" }}>
        <DashboardOverview initialData={data} />
      </main>
    </>
  );
}
