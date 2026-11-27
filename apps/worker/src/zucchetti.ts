import puppeteer from "@cloudflare/puppeteer";
import * as Effect from "effect/Effect";
import type { Browser, Frame, Page } from "@cloudflare/puppeteer";

export class ZucchettiError {
  readonly _tag = "ZucchettiError";
  constructor(readonly reason: string) {}
}

export class BustaNonDisponibile {
  readonly _tag = "BustaNonDisponibile";
  constructor(readonly mese: string) {}
}

export type Busta = {
  bytes: Uint8Array;
  label: string;
  source: { onclick: string; docUrl: string; preaperti: string[] };
};

export interface ZucchettiOptions {
  readonly browser: Browser;
  readonly user: string;
  readonly password: string;
  readonly year: number;
  readonly month: number;
}

const BASE = "https://saas.hrzucchetti.it/hrpprogest";

import { zucchettiLabel } from "./busta.ts";

export const monthLabel = (year: number, month: number): string =>
  zucchettiLabel(new Date(Date.UTC(year, month - 1, 1)));

type EvalPage = Pick<Page, "evaluate">;

async function waitFor(
  page: EvalPage,
  check: (arg: string) => boolean,
  what: string,
  timeoutMs = 15000,
  arg = "",
): Promise<void> {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    if (await page.evaluate(check, arg)) return;
    await new Promise((r) => setTimeout(r, 400));
  }
  throw new ZucchettiError(`timeout: ${what}`);
}

async function clickIf(page: EvalPage, find: () => Element | null, what: string): Promise<void> {
  const ok = await page.evaluate(find, );
  if (!ok) throw new ZucchettiError(`elemento non trovato: ${what}`);
}

async function clickInAnyFrame(
  page: Page,
  act: (text: string) => boolean,
  text: string,
  what: string,
  timeoutMs = 30000,
): Promise<Frame> {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    for (const f of page.frames()) {
      const ok = await f.evaluate(act, text).catch(() => false);
      if (ok) return f;
    }
    await new Promise((r) => setTimeout(r, 500));
  }
  const seen: string[] = [];
  for (const f of page.frames()) {
    const texts = await f
      .evaluate(() =>
        [...document.querySelectorAll("a, [onclick]")]
          .map((x) => (x.textContent || "").trim().replace(/\s+/g, " "))
          .filter((t) => t && t.length < 40)
          .slice(0, 25),
      )
      .catch(() => ["<frame non leggibile>"]);
    seen.push(`${f.url().slice(0, 120)} → ${JSON.stringify(texts)}`);
  }
  throw new ZucchettiError(`elemento non trovato: ${what}\nframe visti:\n${seen.join("\n")}`);
}

async function run(opts: ZucchettiOptions): Promise<Busta> {
  const { browser, user, password } = opts;
  const label = monthLabel(opts.year, opts.month);
  const page = await browser.newPage();
  try {
    await page.goto(`${BASE}/jsp/login.jsp`, { waitUntil: "networkidle2", timeout: 60000 });
    const userField = await page.waitForSelector('input[id$="_m_cUserName"]', { timeout: 30000 });
    await userField!.type(user);
    const passField = await page.waitForSelector('input[type="password"]', { timeout: 10000 });
    await passField!.type(password);
    await clickIf(
      page,
      () => {
        const b = document.querySelector('[id$="_Accedi"]');
        if (!b) return null;
        (b as HTMLElement).click();
        return b;
      },
      "pulsante Accedi",
    );
    await page
      .waitForFunction(() => !location.href.includes("login.jsp"), { timeout: 60000 })
      .catch(() => {
        throw new ZucchettiError("login non riuscita (credenziali o portale cambiato)");
      });

    await clickIf(
      page,
      () => {
        const m = document.querySelector('[id$="_imgMenu"]');
        if (!m) return null;
        (m as HTMLElement).click();
        return m;
      },
      "menu aree applicative",
    );
    await new Promise((r) => setTimeout(r, 800));
    await clickIf(
      page,
      () => {
        const a = [
          ...document.querySelectorAll('[id*="MenuView_AppArea"] a, [id*="MenuView_AppArea"] [id*="_descr"]'),
        ].find((x) => (x.textContent || "").trim() === "Workspace");
        if (!a) return null;
        (a as HTMLElement).click();
        return a;
      },
      "voce Workspace",
    );
    await waitFor(
      page,
      () => [...document.querySelectorAll("iframe")].some((f) => /^MyWorkspace_/.test(f.id)),
      "iframe workspace",
      30000,
    );
    await new Promise((r) => setTimeout(r, 2500));

    await clickInAnyFrame(
      page,
      (t: string) => {
        const a = [...document.querySelectorAll("a, span, div[onclick], li")].find(
          (x) => (x.textContent || "").trim() === t,
        );
        if (!a) return false;
        (a as HTMLElement).click();
        return true;
      },
      "MySpace",
      "tab MySpace",
    );
    await new Promise((r) => setTimeout(r, 2000));

    const frame = await clickInAnyFrame(
      page,
      (t: string) => {
        const a = [...document.querySelectorAll("a")].find(
          (x) => (x.textContent || "").includes(t) && /execLink/.test(x.getAttribute("onclick") || ""),
        );
        if (!a) return false;
        (a as HTMLElement).click();
        return true;
      },
      "I miei documenti",
      "gadget 'I miei documenti'",
    );

    const gridLabel = `Libro unico di ${label}`;
    await waitFor(
      frame,
      (gl: string) =>
        [...document.querySelectorAll("a[onclick]")].some(
          (a) => (a.textContent || "").includes(gl) && /openDocument/.test(a.getAttribute("onclick") || ""),
        ),
      `riga "${gridLabel}"`,
      30000,
      gridLabel,
    );
    const viewers = () =>
      page.evaluate((base: string) => {
        const found: { url: string; title: string }[] = [];
        for (const modal of document.querySelectorAll("iframe")) {
          if (!(modal.src || "").includes("wfolderem_document")) continue;
          const doc = modal.contentDocument;
          if (!doc) continue;
          const title = (doc.body?.innerText || "").match(/Libro unico di \d{2}-\d{4}/)?.[0] ?? "";
          for (const viewer of doc.querySelectorAll("iframe")) {
            const src = viewer.getAttribute("src") || "";
            if (!/viewer\.html/.test(src) || !src.includes("file=")) continue;
            const file = decodeURIComponent(src.split("file=")[1]);
            found.push({ url: new URL(file, `${base}/hrf_pdfviewer/web/`).href, title });
          }
        }
        return found;
      }, BASE);

    const before = new Set((await viewers()).map((v) => v.url));
    const onclick = await frame.evaluate((gl: string) => {
      const a = [...document.querySelectorAll("a[onclick]")].find(
        (x) =>
          (x.textContent || "").includes(gl) && /openDocument/.test(x.getAttribute("onclick") || ""),
      );
      if (!a) return null;
      // Il portale apre la riga *selezionata*, non quella di cui si clicca il
      // link, e click() non la seleziona: senza mousedown/mouseup sulla riga
      // ogni busta tranne la prima apre il documento della prima.
      const win = a.ownerDocument.defaultView as unknown as typeof globalThis;
      for (const type of ["mouseover", "mousedown", "mouseup"]) {
        a.dispatchEvent(new win.MouseEvent(type, { bubbles: true, cancelable: true, view: win as unknown as Window, button: 0 }));
      }
      (a as HTMLElement).click();
      return a.getAttribute("onclick");
    }, gridLabel);
    if (onclick === null) throw new BustaNonDisponibile(gridLabel);

    let opened: { url: string; title: string } | undefined;
    const start = Date.now();
    while (Date.now() - start < 30000) {
      opened = (await viewers()).find((v) => !before.has(v.url) && v.title !== "");
      if (opened) break;
      await new Promise((r) => setTimeout(r, 500));
    }
    if (opened && opened.title !== gridLabel) {
      throw new ZucchettiError(
        `il portale ha aperto "${opened.title}" invece di "${gridLabel}" (onclick=${onclick}): lista riordinata, riprovo`,
      );
    }
    const docUrl = opened?.url;
    if (!docUrl) {
      throw new ZucchettiError(
        `nessun nuovo PDF aperto dopo il click su "${gridLabel}" (onclick=${onclick}, già aperti: ${[...before].join(", ") || "nessuno"})`,
      );
    }

    const b64 = await page.evaluate(async (url: string) => {
      const r = await fetch(url, { credentials: "include" });
      const b = await r.blob();
      return await new Promise<string>((res) => {
        const fr = new FileReader();
        fr.onload = () => res((fr.result as string).split(",")[1]);
        fr.readAsDataURL(b);
      });
    }, docUrl);
    if (typeof b64 !== "string" || !b64.startsWith("JVBERi")) {
      throw new ZucchettiError("download PDF fallito (risposta non valida)");
    }
    const bin = atob(b64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);

    return { bytes, label: `Libro unico di ${label}`, source: { onclick, docUrl, preaperti: [...before] } };
  } finally {
    await page.close().catch(() => {});
  }
}

export const downloadBusta = (opts: ZucchettiOptions): Effect.Effect<Busta, ZucchettiError | BustaNonDisponibile> =>
  Effect.tryPromise({
    try: () => run(opts),
    catch: (error) => {
      if (error instanceof ZucchettiError) return error;
      if (error instanceof BustaNonDisponibile) return error;
      const message = error instanceof Error ? error.message : String(error);
      return new ZucchettiError(message);
    },
  });

export const launchPuppeteer = (browserRun: unknown): Effect.Effect<Browser, ZucchettiError> =>
  Effect.tryPromise({
    try: () => puppeteer.launch(browserRun as never),
    catch: (error) => new ZucchettiError(`launch browser: ${error instanceof Error ? error.message : String(error)}`),
  });
