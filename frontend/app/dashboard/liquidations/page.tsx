import { fetchPerformance } from "../../../lib/api";
import Nav from "../../components/Nav";
import Footer from "../../components/Footer";
import DashSubnav from "../../components/DashSubnav";
import LiquidationFeed from "./LiquidationFeed";

export const dynamic = "force-dynamic";

export default async function LiquidationsPage() {
  const data = await fetchPerformance();
  return (
    <>
      <Nav />
      <DashSubnav />
      <main style={{ paddingTop: 64, minHeight: "100vh" }}>
        <LiquidationFeed initialData={data} />
      </main>
      <Footer />
    </>
  );
}
