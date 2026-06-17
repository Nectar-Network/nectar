import { fetchPerformance } from "../../../lib/api";
import Nav from "../../components/Nav";
import Footer from "../../components/Footer";
import DashSubnav from "../../components/DashSubnav";
import DepositorAnalytics from "../depositor/DepositorAnalytics";

export const dynamic = "force-dynamic";

// Deep link: /dashboard/<G-address> renders the shared depositor analytics
// with the address pre-resolved.
export default async function DepositorDeepLinkPage({
  params,
}: {
  params: { address: string };
}) {
  const data = await fetchPerformance();
  return (
    <>
      <Nav />
      <DashSubnav />
      <main style={{ paddingTop: 64, minHeight: "100vh" }}>
        <DepositorAnalytics initialData={data} initialAddress={params.address} />
      </main>
      <Footer />
    </>
  );
}
