import { fetchPerformance } from "../../lib/api";
import Nav from "../components/Nav";
import Footer from "../components/Footer";
import DashSubnav from "../components/DashSubnav";
import DashboardOverview from "./DashboardOverview";

export const dynamic = "force-dynamic";

export default async function DashboardPage() {
  const data = await fetchPerformance();
  return (
    <>
      <Nav />
      <DashSubnav />
      <main style={{ paddingTop: 64, minHeight: "100vh" }}>
        <DashboardOverview initialData={data} />
      </main>
      <Footer />
    </>
  );
}
