import type { MetadataRoute } from "next";

export default function manifest(): MetadataRoute.Manifest {
  return { name: "PerfectOne ERP", short_name: "PerfectOne", description: "One intelligent platform for complete retail business control", start_url: "/", display: "standalone", background_color: "#070910", theme_color: "#070910", orientation: "any", icons: [{ src: "/icon.svg", sizes: "any", type: "image/svg+xml", purpose: "any" }] };
}
