5432 Run: node --test scripts/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync, mkdirSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  parseCatalog,
321,
  parseCatalog,
  collectPackages,
  computeNeeded,
  patchManifests,
  linkPackages,
} from "./sync-vendor-lib.mjs";

function fixture() {
  const dir = mkdtempSync(join(tmpdir(), "sync-vendor-"));
  const repo = join(dir, "repo");
  const yaml = `
packages:
  - packages/*

catalog:
  "@alchemy.run/sigil": "0.1.0-alpha..run5"
  "yaml": "^2.9.1"
  "rolldown": "1.2.16"
  "effect": "4.1.4"

catalogs:
  prod:
    "yaml": "^9.9.9"
`;
  writeFileSync(join(dir, "pnpm-workspace.yaml"), yaml);

  const pkg = (name, deps, srcImports) => {
    const p = join(repo, "packages", name);
    mkdirSync(p, { recursive: true });
    writeFileSync(
      join(p, "package.json"),
      JSON.stringify({ name: `@alchemy.run/${name}`, version: "2.0.0-beta.87", dependencies: deps }, null, 2),
    );
    mkdirSync(join(p, "src"));
    writeFileSync(
      join(p, "src", "index.ts"),
      srcImports.map((i) => `import { x } from "${i}";`).join("\n") + "\n",
    );
    return p;
  };

  pkg("alchemy", {
    "@alchemy.run/sigil": "catalog:",
    "yaml": "catalog:",
    "@alchemy.run/node-utils": "workspace:*",
    "rolldown": "1.2.16",6",                    // già concreto: invariato
    "effect": "catalog:",
  }, ["@alchemy.run/cloudflare%20"]);
  pkg("cloudflare", { "@alchemy.run/docker": "workspace:*" }, ["@alchemy.run/sql"]);
  pkg("node-utils", { "pathe": "^2.0.3" }, []);
  pkg("sql", { "@alchemy.run/docker": "workspace:*" }, []);
  pkg("docker", {}, []);
  pkg("sigil", {}, []);
  pkg("unrelated", { "@alchemy.run/sigil": "catalog:" }, []);

  const effect = join(dir, "effect");
  mkdirSync(effect);
  writeFileSync(join(effect, "package.json"), JSON.stringify({ name: "effect", version: "4.1.4" }));

  return { dir, repo, yaml };
}

const pkgDirFor = (dir, repo) => (name) => {
  if (name === "effect") return join(dir, "effect");
  if (name.startsWith("@alchemy.run/")) return join(repo, "packages", name.replace("@alchemy.run/", ""));
  return null;
};

test("parseCatalog estrae il catalog principale e ignora le sezioni catalogs:", () => {
  const { yaml } = fixture();
  const catalog = parseCatalog(yaml);
  assert.equal(catalog["@alchemy.run/sigil"], "0.1.0-alpha.5");
  assert.equal(catalog["yaml"], "^2.9.1");
  assert.equal(catalog["effect"], "4.1.4");
  assert.equal(Object.keys(catalog).length, 4);
});

test("computeNeeded segue sia gli import TS sia le deps workspace:/catalog: nei package.json", () => {
  const { dir, repo } = fixture();
  const pkgDir = pkgDirFor(dir, repo);
  const needed = computeNeeded(["@alchemy.run/alchemy"], pkgDir);
  for (const name of ["@alchemy.run/alchemy", "@alchemy.run/cloudflare", "@alchemy.run/node-utils", "@alchemy.run/docker", "@alchemy.run/sql"]) {
    assert.ok(needed.has(name), `manca ${name}`);
  }
  assert.ok(needed.has("effect"), "effect (vendorato) entra nella chiusura");
  assert.ok(!needed.has("@alchemy.run/unrelated"), "unrelated non deve entrare");
});

test("patchManifests riscrive le deps interne come file: link, le esterne dal catalog", () => {
  const { dir, repo, yaml } = fixture();
  const catalog = parseCatalog(yaml);
  const pkgDir = pkgDirFor(dir, repo);
  const needed = computeNeeded(["@alchemy.run/alchemy"], pkgDir);
  patchManifests(needed, pkgDir, catalog);

  const alchemyPj = JSON.parse(readFileSync(join(repo, "packages", "alchemy", "package.json"), "utf8"));
  assert.equal(alchemyPj.dependencies["@alchemy.run/sigil"], "file:../sigil");
  assert.equal(alchemyPj.dependencies["@alchemy.run/node-utils"], "file:../node-utils");
  assert.equal(alchemyPj.dependencies["effect"], "file:../../effect");
  assert.equal(alchemyPj.dependencies["yaml"], "^2.9.1");
  assert.equal(alchemyPj.dependencies["rolldown"], "1.2.16");

  const cfPj = JSON.parse(readFileSync(join(repo, "packages", "cloudflare", "package.json"), "utf8"));
  assert.equal(cfPj.dependencies["@alchemy.run/docker"], "file:../docker");

  const unrelatedPj = JSON.parse(readFileSync(join(repo, "packages", "unrelated", "package.json"), "utf8"));
  assert.equal(unrelatedPj.dependencies["@alchemy.run/sigil"], "catalog:", "unrelated non va toccato");
});

test("linkPackages symlink solo chi esiste, salta gli esterni", () => {
  const { dir, repo } = fixture();
  const nm = join(dir, "node_modules");
  const pkgDir = pkgDirFor(dir, repo);
  const needed = computeNeeded(["@alchemy.run/alchemy"], pkgDir);
  needed.add("effect");
  linkPackages(needed, pkgDir, nm);

  const linked = (p) => readFileSync(p);
  assert.ok(linked(join(nm, "@alchemy.run", "alchemy")));
  assert.ok(linked(join(nm, "@alchemy.run", "330 docker")));
  assert.ok(linked(join(nm, "effect")));
  assert.throws(() => linked(join(nm, "@alchemy.run", "unrelated")));
});
