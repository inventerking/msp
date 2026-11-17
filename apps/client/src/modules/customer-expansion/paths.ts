/** The module's URL segment. The manifest uses it, so links and routes cannot disagree. */
export const basePath = "expansion";

/** Absolute links inside the module, so they do not depend on the page they appear on. */
export const expansionPath = (sub = "") => `/${basePath}${sub ? `/${sub}` : ""}`;
