#!/usr/bin/env bun

import { existsSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import ts from "typescript";

type Args = {
  packages: string[];
  targets: string[];
  strict: boolean;
};

type ExportedSymbol = {
  file: string;
  name: string;
  kind: string;
  line: number;
};

type UseSite = {
  fromFile: string;
  kind: "import" | "namespace";
};

const repoRoot = process.cwd();
const defaultPackages = [
  "packages/data",
  "packages/gui-agent",
  "packages/foxterm",
  "packages/gui-auth-gateway",
];
const defaultTargets = [
  "packages/data/src/client.ts",
  "packages/data/src/orchestration.ts",
  "packages/gui-agent/src/api/client.ts",
];

function usage(): never {
  console.error(`Usage:
  bun scripts/check-frontend-dead-exports.ts [--packages a,b,c] [--targets file.ts,file2.ts] [--strict]

Checks exported declarations from targeted frontend modules against imports and
namespace property references in the active frontend package graph. The default
mode is report-only; --strict exits non-zero when findings exist.
`);
  process.exit(1);
}

function parseArgs(argv: string[]): Args {
  const args: Args = {
    packages: defaultPackages,
    targets: defaultTargets,
    strict: false,
  };

  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--strict") {
      args.strict = true;
      continue;
    }
    if (arg === "--packages" || arg === "--targets") {
      const value = argv[i + 1];
      if (!value || value.startsWith("--")) usage();
      const entries = value
        .split(",")
        .map((entry) => entry.trim())
        .filter(Boolean);
      if (entries.length === 0) usage();
      if (arg === "--packages") args.packages = entries;
      if (arg === "--targets") args.targets = entries;
      i += 1;
      continue;
    }
    usage();
  }

  return args;
}

function repoPath(input: string): string {
  return normalizePath(path.relative(repoRoot, input));
}

function normalizePath(input: string): string {
  return input.split(path.sep).join("/");
}

function absolutePath(input: string): string {
  return path.resolve(repoRoot, input);
}

function isSourceFile(filePath: string): boolean {
  return /\.(ts|tsx)$/.test(filePath) && !filePath.endsWith(".d.ts");
}

function shouldSkip(filePath: string): boolean {
  const normalized = normalizePath(filePath);
  return (
    normalized.includes("/node_modules/") ||
    normalized.includes("/dist/") ||
    normalized.includes("/docs/archive/") ||
    normalized.endsWith(".tsbuildinfo")
  );
}

function walkSourceFiles(dir: string, out: string[]): void {
  if (!existsSync(dir)) return;
  for (const entry of readdirSync(dir)) {
    const fullPath = path.join(dir, entry);
    if (shouldSkip(fullPath)) continue;
    const stat = statSync(fullPath);
    if (stat.isDirectory()) {
      walkSourceFiles(fullPath, out);
      continue;
    }
    if (stat.isFile() && isSourceFile(fullPath)) {
      out.push(path.resolve(fullPath));
    }
  }
}

function compilerOptions(): ts.CompilerOptions {
  return {
    allowImportingTsExtensions: true,
    baseUrl: repoRoot,
    jsx: ts.JsxEmit.ReactJSX,
    module: ts.ModuleKind.ESNext,
    moduleResolution: ts.ModuleResolutionKind.Bundler,
    noEmit: true,
    paths: {
      "@/*": ["packages/gui-agent/src/*"],
      "@foxctl/data": ["packages/data/src/index.ts"],
      "@foxctl/data/client": ["packages/data/src/client.ts"],
      "@foxctl/data/orchestration": ["packages/data/src/orchestration.ts"],
      "@foxctl/data/types": ["packages/data/src/types.ts"],
    },
    skipLibCheck: true,
    strict: true,
    target: ts.ScriptTarget.ES2022,
  };
}

function hasExportModifier(node: ts.Node): boolean {
  return Boolean(
    ts.canHaveModifiers(node) &&
      ts.getModifiers(node)?.some((modifier) => modifier.kind === ts.SyntaxKind.ExportKeyword),
  );
}

function declarationKind(node: ts.Node): string {
  if (ts.isFunctionDeclaration(node)) return "function";
  if (ts.isClassDeclaration(node)) return "class";
  if (ts.isInterfaceDeclaration(node)) return "interface";
  if (ts.isTypeAliasDeclaration(node)) return "type";
  if (ts.isEnumDeclaration(node)) return "enum";
  if (ts.isVariableStatement(node)) return "const";
  return "export";
}

function lineOf(sourceFile: ts.SourceFile, node: ts.Node): number {
  return sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile)).line + 1;
}

function addExport(
  exportsByFile: Map<string, Map<string, ExportedSymbol>>,
  directExportsByFile: Map<string, Set<string>>,
  sourceFile: ts.SourceFile,
  name: string,
  kind: string,
  node: ts.Node,
): void {
  const file = repoPath(sourceFile.fileName);
  let exportsForFile = exportsByFile.get(file);
  if (!exportsForFile) {
    exportsForFile = new Map();
    exportsByFile.set(file, exportsForFile);
  }
  exportsForFile.set(name, {
    file,
    name,
    kind,
    line: lineOf(sourceFile, node),
  });

  let directExports = directExportsByFile.get(file);
  if (!directExports) {
    directExports = new Set();
    directExportsByFile.set(file, directExports);
  }
  directExports.add(name);
}

function collectExports(
  program: ts.Program,
  sourceFiles: ts.SourceFile[],
): {
  exportsByFile: Map<string, Map<string, ExportedSymbol>>;
  directExportsByFile: Map<string, Set<string>>;
  starExportsByFile: Map<string, string[]>;
} {
  const exportsByFile = new Map<string, Map<string, ExportedSymbol>>();
  const directExportsByFile = new Map<string, Set<string>>();
  const starExportsByFile = new Map<string, string[]>();
  const options = program.getCompilerOptions();
  const host = ts.createCompilerHost(options);

  for (const sourceFile of sourceFiles) {
    const file = repoPath(sourceFile.fileName);
    for (const statement of sourceFile.statements) {
      if (
        (ts.isFunctionDeclaration(statement) ||
          ts.isClassDeclaration(statement) ||
          ts.isInterfaceDeclaration(statement) ||
          ts.isTypeAliasDeclaration(statement) ||
          ts.isEnumDeclaration(statement)) &&
        hasExportModifier(statement) &&
        statement.name
      ) {
        addExport(
          exportsByFile,
          directExportsByFile,
          sourceFile,
          statement.name.text,
          declarationKind(statement),
          statement.name,
        );
        continue;
      }

      if (ts.isVariableStatement(statement) && hasExportModifier(statement)) {
        for (const declaration of statement.declarationList.declarations) {
          if (ts.isIdentifier(declaration.name)) {
            addExport(
              exportsByFile,
              directExportsByFile,
              sourceFile,
              declaration.name.text,
              declarationKind(statement),
              declaration.name,
            );
          }
        }
        continue;
      }

      if (ts.isExportDeclaration(statement)) {
        if (!statement.exportClause && statement.moduleSpecifier) {
          const resolved = resolveModule(
            statement.moduleSpecifier,
            sourceFile.fileName,
            options,
            host,
          );
          if (resolved) {
            const targets = starExportsByFile.get(file) ?? [];
            targets.push(resolved);
            starExportsByFile.set(file, targets);
          }
        }
        if (statement.exportClause && ts.isNamedExports(statement.exportClause)) {
          for (const specifier of statement.exportClause.elements) {
            addExport(
              exportsByFile,
              directExportsByFile,
              sourceFile,
              specifier.name.text,
              "re-export",
              specifier.name,
            );
          }
        }
      }
    }
  }

  return { exportsByFile, directExportsByFile, starExportsByFile };
}

function resolveModule(
  moduleSpecifier: ts.Expression,
  containingFile: string,
  options: ts.CompilerOptions,
  host: ts.ModuleResolutionHost,
): string | null {
  if (!ts.isStringLiteralLike(moduleSpecifier)) return null;
  const resolved = ts.resolveModuleName(
    moduleSpecifier.text,
    containingFile,
    options,
    host,
  ).resolvedModule?.resolvedFileName;
  if (!resolved || shouldSkip(resolved) || !isSourceFile(resolved)) return null;
  return repoPath(resolved);
}

function resolveOrigin(
  file: string,
  name: string,
  directExportsByFile: Map<string, Set<string>>,
  starExportsByFile: Map<string, string[]>,
  seen = new Set<string>(),
): string | null {
  const key = `${file}#${name}`;
  if (seen.has(key)) return null;
  seen.add(key);

  if (directExportsByFile.get(file)?.has(name)) return file;

  for (const target of starExportsByFile.get(file) ?? []) {
    const origin = resolveOrigin(target, name, directExportsByFile, starExportsByFile, seen);
    if (origin) return origin;
  }

  return null;
}

function recordUse(
  usesByExport: Map<string, UseSite[]>,
  targetFile: string,
  exportName: string,
  fromFile: string,
  kind: UseSite["kind"],
): void {
  if (targetFile === fromFile) return;
  const key = `${targetFile}#${exportName}`;
  const uses = usesByExport.get(key) ?? [];
  if (!uses.some((use) => use.fromFile === fromFile && use.kind === kind)) {
    uses.push({ fromFile, kind });
  }
  usesByExport.set(key, uses);
}

function collectUses(
  program: ts.Program,
  sourceFiles: ts.SourceFile[],
  directExportsByFile: Map<string, Set<string>>,
  starExportsByFile: Map<string, string[]>,
): Map<string, UseSite[]> {
  const usesByExport = new Map<string, UseSite[]>();
  const options = program.getCompilerOptions();
  const host = ts.createCompilerHost(options);

  for (const sourceFile of sourceFiles) {
    const fromFile = repoPath(sourceFile.fileName);
    const namespaceImports = new Map<string, string>();

    for (const statement of sourceFile.statements) {
      if (ts.isImportDeclaration(statement)) {
        const resolved = resolveModule(
          statement.moduleSpecifier,
          sourceFile.fileName,
          options,
          host,
        );
        if (!resolved) continue;
        const importClause = statement.importClause;
        if (!importClause) continue;

        const bindings = importClause.namedBindings;
        if (bindings && ts.isNamedImports(bindings)) {
          for (const specifier of bindings.elements) {
            const importedName = specifier.propertyName?.text ?? specifier.name.text;
            const origin =
              resolveOrigin(resolved, importedName, directExportsByFile, starExportsByFile) ??
              resolved;
            recordUse(usesByExport, origin, importedName, fromFile, "import");
          }
        }

        if (bindings && ts.isNamespaceImport(bindings)) {
          namespaceImports.set(bindings.name.text, resolved);
        }
      }
    }

    const visit = (node: ts.Node): void => {
      if (ts.isPropertyAccessExpression(node) && ts.isIdentifier(node.expression)) {
        const target = namespaceImports.get(node.expression.text);
        if (target) {
          const exportName = node.name.text;
          const origin =
            resolveOrigin(target, exportName, directExportsByFile, starExportsByFile) ?? target;
          recordUse(usesByExport, origin, exportName, fromFile, "namespace");
        }
      }
      if (ts.isQualifiedName(node) && ts.isIdentifier(node.left)) {
        const target = namespaceImports.get(node.left.text);
        if (target) {
          const exportName = node.right.text;
          const origin =
            resolveOrigin(target, exportName, directExportsByFile, starExportsByFile) ?? target;
          recordUse(usesByExport, origin, exportName, fromFile, "namespace");
        }
      }
      ts.forEachChild(node, visit);
    };
    visit(sourceFile);
  }

  return usesByExport;
}

function main(): void {
  const args = parseArgs(process.argv.slice(2));
  const sourceFilePaths: string[] = [];
  for (const packageDir of args.packages) {
    walkSourceFiles(path.join(absolutePath(packageDir), "src"), sourceFilePaths);
  }

  const program = ts.createProgram(sourceFilePaths, compilerOptions());
  const sourceFileSet = new Set(sourceFilePaths.map((file) => repoPath(file)));
  const sourceFiles = program
    .getSourceFiles()
    .filter((sourceFile) => sourceFileSet.has(repoPath(sourceFile.fileName)));

  const { exportsByFile, directExportsByFile, starExportsByFile } = collectExports(
    program,
    sourceFiles,
  );
  const usesByExport = collectUses(
    program,
    sourceFiles,
    directExportsByFile,
    starExportsByFile,
  );

  const targetSet = new Set(args.targets.map((target) => repoPath(absolutePath(target))));
  const findings: ExportedSymbol[] = [];
  for (const target of targetSet) {
    const exportsForFile = exportsByFile.get(target);
    if (!exportsForFile) continue;
    for (const exported of exportsForFile.values()) {
      if (!usesByExport.has(`${target}#${exported.name}`)) {
        findings.push(exported);
      }
    }
  }

  if (findings.length === 0) {
    console.log("No externally unused frontend exports found in targeted modules.");
    return;
  }

  console.log("Externally unused frontend exports in targeted modules:");
  let currentFile = "";
  for (const finding of findings.sort((left, right) =>
    left.file === right.file ? left.line - right.line : left.file.localeCompare(right.file),
  )) {
    if (finding.file !== currentFile) {
      currentFile = finding.file;
      console.log(`\n${currentFile}`);
    }
    console.log(`  ${finding.line}: ${finding.kind} ${finding.name}`);
  }

  console.log(
    `\n${findings.length} finding(s). Default mode is report-only; pass --strict to fail on findings.`,
  );
  if (args.strict) {
    process.exitCode = 1;
  }
}

main();
