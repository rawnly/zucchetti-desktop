import * as Cloudflare from "@alchemy.run/cloudflare";
import * as Config from "effect/Config";
import * as Effect from "effect/Effect";
import * as HttpServerRequest from "effect/http/HttpServerRequest";
import * as HttpServerResponse from "effect/http/HttpServerResponse";
import * as Redacted from "effect/Redacted";
import { isBustaKey, parseBustaKey, timingSafeTokenEqual } from "./busta.ts";
import BustaWorkflow, { Buste } from "./workflow.ts";

export default Cloudflare.Worker(
  "BustePagaWorker",
  {
    main: import.meta.url,
    compatibility: {
      flags: ["nodejs_compat"],
      date: "2026-11-01",
    },
  },
  Effect.gen(function* () {
    const workflow = yield* BustaWorkflow;
    const bucket = yield* Cloudflare.R2.ReadWriteBucket(Buste);
    const runToken = yield* Config.Redacted("RUN_TOKEN");

    return {
      fetch: Effect.gen(function* () {
        const request = yield* HttpServerRequest.HttpServerRequest;
        const url = new URL(request.url, "http://localhost");
        const authorized = yield* Effect.promise(() =>
          timingSafeTokenEqual(request.headers["x-run-token"], Redacted.value(runToken)),
        );
        if (!authorized) {
          return HttpServerResponse.text(
            "Buste Paga worker. GET /buste, GET /buste/<key>, POST /run[?date=YYYY-MM-DD] o GET /status/<id>, con header x-run-token.",
            { status: 404 },
          );
        }

        if (request.method === "GET" && url.pathname === "/buste") {
          const items: {
            key: string;
            year: number;
            month: number;
            size: number;
            uploaded: string;
            etag: string;
          }[] = [];
          let cursor: string | undefined;
          do {
            const page = yield* bucket.list({ prefix: "busta-", cursor });
            for (const obj of page.objects) {
              const parsed = parseBustaKey(obj.key);
              if (!parsed) continue;
              items.push({
                key: obj.key,
                year: parsed.year,
                month: parsed.month,
                size: obj.size,
                uploaded: obj.uploaded.toISOString(),
                etag: obj.httpEtag,
              });
            }
            cursor = page.truncated ? page.cursor : undefined;
          } while (cursor !== undefined);
          items.sort((a, b) => b.key.localeCompare(a.key));
          return HttpServerResponse.jsonUnsafe({ items });
        }

        if (request.method === "GET" && url.pathname.startsWith("/buste/")) {
          let key: string;
          try {
            key = decodeURIComponent(url.pathname.slice("/buste/".length));
          } catch {
            return HttpServerResponse.text("Bad Request", { status: 400 });
          }
          if (!isBustaKey(key)) {
            return HttpServerResponse.text("Not Found", { status: 404 });
          }
          const obj = yield* bucket.get(key);
          if (obj === null) {
            return HttpServerResponse.text("Not Found", { status: 404 });
          }
          const bytes = yield* obj.bytes();
          return HttpServerResponse.uint8Array(bytes, {
            contentType: "application/pdf",
            headers: {
              "content-disposition": `attachment; filename="${key}"`,
              etag: obj.httpEtag,
              "cache-control": "private, max-age=3600",
            },
          });
        }

        if (request.method === "POST" && url.pathname === "/run") {
          const date = url.searchParams.get("date") ?? undefined;
          const notify = url.searchParams.get("notify") !== "0";
          const instance = yield* workflow.create({ params: { date, notify } });
          yield* Effect.log(`trigger manuale: workflow ${instance.id}${date ? ` (date=${date})` : ""}`);
          return HttpServerResponse.jsonUnsafe({ instanceId: instance.id });
        }

        if (request.method === "GET" && url.pathname.startsWith("/status/")) {
          const instance = yield* workflow.get(url.pathname.slice("/status/".length));
          return HttpServerResponse.jsonUnsafe(yield* instance.status());
        }

        return HttpServerResponse.text("Not Found", { status: 404 });
      }).pipe(
        Effect.catchTag("R2Error", (err) =>
          Effect.gen(function* () {
            yield* Effect.logError(`R2 error: ${err.message}`);
            return HttpServerResponse.jsonUnsafe(
              { error: "Storage error" },
              { status: 502 },
            );
          }),
        ),
      ),
    };
  }).pipe(
    Effect.provide(Cloudflare.Workers.BrowserBinding),
    Effect.provide(Cloudflare.R2.ReadWriteBucketBinding),
    Effect.provide(Cloudflare.Email.SendBinding),
  ),
);
