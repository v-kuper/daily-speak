import { existsSync, readFileSync } from "node:fs";
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
    const localRequire = (specifier) => {
      if (!specifier.startsWith(".")) return require(specifier);
      const base = resolve(dirname(filename), specifier);
      const source = [base, `${base}.ts`, `${base}.tsx`, `${base}/index.ts`]
        .find((candidate) => /\.tsx?$/.test(candidate) && existsSync(candidate));
      if (!source) throw new Error(`Cannot resolve ${specifier} from ${filename}`);
      return load(source);
    };
    const { outputText } = ts.transpileModule(readFileSync(filename, "utf8"), {
      fileName: filename,
      compilerOptions: {
        module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022,
        jsx: ts.JsxEmit.ReactJSX, esModuleInterop: true,
      },
    });
    runInThisContext(`(function(require, module, exports) {\n${outputText}\n})`, { filename })(localRequire, loadedModule, loadedModule.exports);
    return loadedModule.exports;
  };
  return load;
}
