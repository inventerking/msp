/** Frontend boundaries. Each rule fails the build. */
module.exports = {
  forbidden: [
    { name: "no-cross-module-imports", severity: "error", comment: "A feature module may not import another feature module.",
      from: { path: "^apps/client/src/modules/([^/]+)/" }, to: { path: "^apps/client/src/modules/", pathNot: ["^apps/client/src/modules/$1/", "^apps/client/src/modules/types\\.ts$"] } },
    { name: "shell-uses-manifests-only", severity: "error", comment: "The shell knows a module only through its manifest.",
      from: { path: "^apps/client/src/(shell|platform|app)/" }, to: { path: "^apps/client/src/modules/[^/]+/(routes|api|components|state)/" } },
    { name: "apps-are-separate", severity: "error", comment: "Client and admin share packages, never code.",
      from: { path: "^apps/client/" }, to: { path: "^apps/admin/" } },
    { name: "apps-are-separate-2", severity: "error", from: { path: "^apps/admin/" }, to: { path: "^apps/client/" } },
    { name: "packages-do-not-import-apps", severity: "error", from: { path: "^packages/" }, to: { path: "^apps/" } },
    { name: "only-api-client-uses-http", severity: "error", comment: "Only @msp/api-client talks to the network.",
      from: { path: "^(apps|packages)/", pathNot: "^packages/api-client/" }, to: { path: "openapi-fetch" } },
    { name: "no-circular", severity: "error", from: { path: "^(apps|packages)/" }, to: { circular: true } },
  ],
  options: { doNotFollow: { path: "node_modules" }, exclude: { path: "(dist|\\.gen\\.ts$)" }, tsPreCompilationDeps: false, enhancedResolveOptions: { exportsFields: ["exports"], conditionNames: ["import", "default"], extensions: [".ts", ".tsx", ".js"] } },
};
