import { fetchPerformance } from "../../lib/api";
import Nav from "../components/Nav";
import Footer from "../components/Footer";
import PerformanceDashboard from "./PerformanceDashboard";

export const dynamic = "force-dynamic";

export default async function PerformancePage() {
  const data = await fetchPerformance();

  return (
    <>
      <Nav />
      <main style={{ paddingTop: 64, minHeight: "100vh" }}>
        <PerformanceDashboard initialData={data} />
      </main>
      <Footer />
    </>
  );
}
