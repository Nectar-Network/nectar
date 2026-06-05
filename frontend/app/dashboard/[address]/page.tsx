import { fetchPerformance } from "../../../lib/api";
import Nav from "../../components/Nav";
import DepositorAnalytics from "./DepositorAnalytics";

export const dynamic = "force-dynamic";

export default async function DepositorPage({ params }: { params: { address: string } }) {
  const data = await fetchPerformance();
  return (
    <>
      <Nav />
      <main style={{ paddingTop: "80px", minHeight: "100vh" }}>
        <DepositorAnalytics address={params.address} initialData={data} />
      </main>
    </>
  );
}
