import { readFileSync, writeFileSync, readdirSync, statSync, symlinkSync, mkdirSync, existsSync } from "node:fs";
import { dirname, join } from "node:path";

export function parseCatalog(yamlText) {
  const catalog = {};
  let inCatalog = false;
  for (const line of yamlText.split("\n")) {
    if (/^catalog:/.test(line)) { inCatalog = true; continue; }
    if (/^catalogs:/.test(line)) { inCatalog = false; continue; }
    if (inCatalog && /^\S/.test(line)) inCatalog = false;
    if (!inCatalog) continue;
    const m = line.match(/^\s+"?([@a-z0-9/.-]+)"?:\s*"([^"]+)"/);
    if (m) catalog[m[1]] = m[2];
  }
  return catalog;
}

export function collectPackages(repoDir) {
  const index = {};
  const pkgs = join(repoDir, "packages");
  if (!existsSync(pkgs)) return index;
  for (const entry of readdirSync(pkgs)) {
    const pj = join(pkgs, entry, "package.json");
    if (!existsSync(pj)) continue;
    try {
      const name = JSON.parse(readFileSync(pj, "utf8")).name;
      if (name) index[name] = join(pkgs, entry);
    } catch {}
  }
  return index;
}

export function scanImports(dir, acc) {
  for (const entry of readdirSync(dir)) {
    const p = join(dir, entry);
    let s;
    try { s = statSync(p); } catch { continue; }
    if (s.isDirectory()) { if (entry !== "node_modules" && entry !== "dist") scanImports(p, acc); continue; }
    if (!entry.endsWith(".ts")) continue;
    const src = readFileSync(p, "utf8");
    for (const m of src.matchAll(/from "(@alchemy\.run\/[a-z0-9-]+)(?:\/[^"]*)?"/g)) acc.add(m[1]);
    for (const m of src.matchAll(/from "alchemy\/[A-Za-z0-9-]+"/g)) acc.add("alchemy");
  }
}

export function computeNeeded(roots, pkgDir) {
  const needed = new Set();
  const queue = [...roots];
  const seen = new Set();
  while (queue.length) {
    const name = queue.shift();
    if (seen.has(name)) continue;
    seen.add(name);
    const dir = pkgDir(name);
    if (!dir) continue;
    needed.add(name);
    const found = new Set();
    scanImports(dir, found);
    const pjPath = join(dir, "package.json");
    if (existsSync(pjPath)) {
      const pj = JSON.parse(readFileSync(pjPath, "utf8"));
      for (const section of ["dependencies", "peerDependencies"]) {
        for (const [dep, spec] of Object.entries(pj[section] ?? {})) {
          if (
            typeof spec === "string" &&
            (spec.startsWith("workspace:") || spec === "catalog:" || spec.startsWith("catalog:"))
          ) {
            found.add(dep);
          }
        }
      }
    }
    for (const dep of found) if (!seen.has(dep)) queue.push(dep);
  }
  return needed;
}

export function patchManifests(needed, pkgDir, catalog) {
  const versionOf = (name) => {
    const dir = pkgDir(name);
    if (!dir) return null;
    try { return JSON.parse(readFileSync(join(dir, "package.json"), "utf8")).version; } catch { return null; }
  };
  const patched = [];
  for (const name of needed) {
    const dir = pkgDir(name);
    if (!dir) continue;
    const pjPath = join(dir, "package.json");
    const pj = JSON.parse(readFileSync(pjPath, "utf8"));
    for (const section of ["dependencies", "peerDependencies", "devDependencies"]) {
      const deps = pj[section];
      if (!deps) continue;
      for (const [dep, spec] of Object.entries(deps)) {
        if (typeof spec !== "string") continue;
        if (spec.startsWith("workspace:")) {
          const v = versionOf(dep);
          deps[dep] = v ?? "*";
        } else if (spec === "catalog:" || spec.startsWith("catalog:")) {
          if (catalog[dep]) deps[dep] = catalog[dep];
        }
      }
    }
    writeFileSync(pjPath, JSON.stringify(pj, null, 2) + "\n");
    patched.push(name);
  }
  return patched;
}

export function linkPackages(needed, pkgDir, nmDir) {
  const linked = [];
  for (const name of needed) {
    const target = pkgDir(name);
    if (!target) continue;
    const dest = join(nmDir, name);
    mkdirSync(dirname(dest), { recursive: true });
    if (existsSync(dest)) continue;
    symlinkSync(target, dest);
    linked.push(name);
  }
  return linked;
}

export function applyVendorPatch(path, from, to) {
  const src = readFileSync(path, "utf8");
  if (src.includes(to)) return false;
  if (!src.includes(from)) {
    throw new Error(`patch non applicabile (pattern assente): ${path}`);
  }
  writeFileSync(path, src.replace(from, to));
  return true;
}
