import Nav from "../components/Nav";
import FeaturesContent from "./FeaturesContent";
import Footer from "../components/Footer";

export const dynamic = "force-dynamic";

export default function FeaturesPage() {
  return (
    <>
      <Nav />
      <main style={{ paddingTop: 64, minHeight: "100vh" }}>
        <FeaturesContent />
      </main>
      <Footer />
    </>
  );
}
