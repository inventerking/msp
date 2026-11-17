/** The module's URL segment. The manifest uses it, so links and routes cannot disagree. */
export const basePath = "insights";

/** Absolute links inside the module, so they do not depend on the page they appear on. */
export const intelPath = (sub = "") => `/${basePath}${sub ? `/${sub}` : ""}`;
