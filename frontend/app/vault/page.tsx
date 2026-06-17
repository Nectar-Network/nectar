import Nav from "../components/Nav";
import Footer from "../components/Footer";
import VaultApp from "./VaultApp";

export const dynamic = "force-dynamic";

export default function VaultPage() {
  return (
    <>
      <Nav />
      <main style={{ paddingTop: 64, minHeight: "100vh" }}>
        <VaultApp />
      </main>
      <Footer />
    </>
  );
}
