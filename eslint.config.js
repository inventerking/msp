import tseslint from "typescript-eslint";

const noRawHtml = {
  selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']",
  message: "Do not render raw HTML. Use the sanitizer in @msp/ui.",
};

export default tseslint.config(
  { ignores: ["**/dist/**", "**/node_modules/**", "**/*.gen.ts", "backend/**", "docs/**", "**/playwright-report/**", "**/test-results/**"] },
  ...tseslint.configs.recommended,
  {
    rules: {
      "no-restricted-syntax": ["error", noRawHtml],
      "@typescript-eslint/no-unused-vars": ["error", { argsIgnorePattern: "^_" }],
    },
  },
  {
    // All network calls go through the generated client, so the contract is always checked.
    files: ["apps/client/src/**", "apps/admin/src/**"],
    rules: { "no-restricted-globals": ["error", { name: "fetch", message: "Use the API client from @msp/api-client." }, { name: "localStorage", message: "Do not store session or tenant data in the browser." }, { name: "sessionStorage", message: "Do not store session or tenant data in the browser." }] },
  },
);
