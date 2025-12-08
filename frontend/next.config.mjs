/** @type {import('next').NextConfig} */
const nextConfig = {
  rewrites: async () => {
    return [
      {
        source: "/api/v1/:path*",
        destination: `${process.env.NEXT_PUBLIC_API_URL}/:path*`,
      },
    ];
  },
  output: "export",
  distDir: "build",
  trailingSlash: true,
  basePath: process.env.NEXT_PUBLIC_BASE_PATH || "",
  assetPrefix: process.env.NEXT_PUBLIC_BASE_PATH || "",
  images: {
    unoptimized: true,
  },
};

export default nextConfig;
