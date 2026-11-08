// The bundler turns an import of a model or an image into its URL and copies the file into the
// build (see src/scene/castleAssets.ts).
declare module "*.glb" {
  const url: string;
  export default url;
}

declare module "*.png" {
  const url: string;
  export default url;
}
