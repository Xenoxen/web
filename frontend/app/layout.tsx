import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "OCAP2",
  description: "OCAP 2.1",
  icons: [
    {
      rel: "icon",
      url: "/favicon.png"
    },
  ],
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <body className="h-screen max-h-screen overflow-y-hidden">
        {children}
      </body>
    </html>
  );
}
