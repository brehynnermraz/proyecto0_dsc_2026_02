/** @type {import('next').NextConfig} */
const nextConfig = {
  // Genera un servidor mínimo autocontenido en .next/standalone (con solo las
  // dependencias que realmente usa), para una imagen Docker pequeña. Sin esto,
  // el Dockerfile tendría que arrastrar todo node_modules.
  output: "standalone",
};

export default nextConfig;
