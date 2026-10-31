// @kabelsalat/web 0.4.1 (a dependency of @strudel/core) has no "exports" map:
// Node picks its CommonJS "main" and misses the named exports Strudel imports
// ("does not provide an export named 'SalatRepl'"). Point Node at its ESM build.
export async function resolve(specifier, context, next) {
  if (specifier === "@kabelsalat/web") return next("@kabelsalat/web/dist/index.mjs", context);
  return next(specifier, context);
}
