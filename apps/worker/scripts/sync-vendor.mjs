#!/usr/bin/env node
import { readFileSync } from "node:fs";
import { resolve, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { parseCatalog, collectPackages, computeNeeded, patchManifests, applyVendorPatch } from "./sync-vendor-lib.mjs";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const vendorDir = join(root, "vendor");
const REPO = "alchemy-repo";

const repoDir = join(vendorDir, REPO);
const catalog = parseCatalog(readFileSync(join(repoDir, "pnpm-workspace.yaml"), "utf8"));
const packages = collectPackages(repoDir);
const pkgDir = (name) => name === "effect" ? join(vendorDir, "effect") : packages[name] ?? null;

const needed = computeNeeded(["alchemy", "@alchemy.run/cloudflare"], pkgDir);
needed.add("effect");

console.log("pacchetti interni:", [...needed].sort().join(", "));

const patched = patchManifests(needed, pkgDir, catalog);
console.log("patch:", patched.join(", "));

const mcpPatched = applyVendorPatch(
  join(vendorDir, "effect", "src", "ai", "McpClient.ts"),
  `    if ("error" in response) {`,
  `    if ("error" in response && response.error !== undefined) {`,
);
console.log("patch McpClient:", mcpPatched ? "applicata" : "già presente");

console.log("\nfatto. ora: npm install (crea i symlink dalle deps file:)");
