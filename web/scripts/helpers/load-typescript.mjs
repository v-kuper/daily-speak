import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, resolve } from "node:path";
import { runInThisContext } from "node:vm";
import ts from "typescript";

// Execute real application modules and their local dependencies without a server.
export function createTypeScriptLoader() {
  const cache = new Map();
  const load = (path) => {
    const filename = resolve(path);
    if (cache.has(filename)) return cache.get(filename).exports;
    const loadedModule = { exports: {} };
    cache.set(filename, loadedModule);
    const require = createRequire(filename);
    const localRequire = (specifier) => specifier.startsWith(".")
      ? load(resolve(dirname(filename), `${specifier}.ts`))
      : require(specifier);
    const { outputText } = ts.transpileModule(readFileSync(filename, "utf8"), {
      compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
    });
    runInThisContext(`(function(require, module, exports) {\n${outputText}\n})`, { filename })(localRequire, loadedModule, loadedModule.exports);
    return loadedModule.exports;
  };
  return load;
}
