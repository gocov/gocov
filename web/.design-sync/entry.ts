// The design-system entry the /design-sync converter bundles.
//
// web/ is an application, not a published library, so there is no dist entry
// to point the converter at. This file is that entry: everything the four
// atomic layers export, plus the one wrapper preview cards need in order to
// render (components reach for a router and a query client).
//
// Importing base.css here is what puts the tokens, the layout primitives and
// every component's own stylesheet into `_ds_bundle.css` — the file
// `styles.css` @imports, which is the only CSS a rendered design receives.
import "../src/styles/base.css";

export * from "../src/components/atoms";
export * from "../src/components/molecules";
export * from "../src/components/organisms";
export * from "../src/components/templates";

export { PreviewProviders } from "./preview-providers";
