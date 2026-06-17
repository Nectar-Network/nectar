import type { Metadata } from "next";
import Nav from "../components/Nav";
import Footer from "../components/Footer";
import MediaKitContent from "./MediaKitContent";

export const metadata: Metadata = {
  title: "Nectar — Media Kit",
  description:
    "Nectar brand assets: the hive mark, lockup, app icon and color — downloadable in SVG, PNG and JPG, with usage rules.",
};

export default function MediaKitPage() {
  return (
    <>
      <Nav />
      <main style={{ paddingTop: "80px", minHeight: "100vh" }}>
        <MediaKitContent />
      </main>
      <Footer />
    </>
  );
}
