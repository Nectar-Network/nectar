import { fetchPerformance } from "../../../lib/api";
import Nav from "../../components/Nav";
import Footer from "../../components/Footer";
import DashSubnav from "../../components/DashSubnav";
import DepositorAnalytics from "./DepositorAnalytics";

export const dynamic = "force-dynamic";

export default async function DepositorPage() {
  const data = await fetchPerformance();
  return (
    <>
      <Nav />
      <DashSubnav />
      <main style={{ paddingTop: 64, minHeight: "100vh" }}>
        <DepositorAnalytics initialData={data} />
      </main>
      <Footer />
    </>
  );
}
