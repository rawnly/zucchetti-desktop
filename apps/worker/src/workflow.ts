import * as Cloudflare from "@alchemy.run/cloudflare";
import * as Config from "effect/Config";
import * as Effect from "effect/Effect";
import * as Redacted from "effect/Redacted";
import { bustaKey, parseBustaKey, prevBustaKey, sameBytes, zucchettiLabel } from "./busta.ts";
import { downloadBusta, launchPuppeteer } from "./zucchetti.ts";

export const Buste = Cloudflare.R2.Bucket("BustePaga");

export const Email = Cloudflare.Email.SendEmail("Email");

export const SCHEDULES = ["0 16 27 * *", "0 17 27 * *"];

const ORA_ROMA = 18;

const oraRoma = (d: Date): number =>
  Number(new Intl.DateTimeFormat("en-GB", { timeZone: "Europe/Rome", hour: "numeric", hourCycle: "h23" }).format(d));

export interface BustaInput {
  readonly date?: string;
  /** false: salta l'email con l'allegato (re-fetch manuale dall'app desktop). */
  readonly notify?: boolean;
}

const asError = (e: unknown): Error =>
  e instanceof Error ? new Error(e.message) : new Error(typeof e === "object" ? JSON.stringify(e) : String(e));

export default class BustaWorkflow extends Cloudflare.Workflow<BustaWorkflow>()(
  "BustaWorkflow",
  { schedules: SCHEDULES },
  Effect.gen(function* () {
    const browser = yield* Cloudflare.Browser("BROWSER");
    const bucket = yield* Cloudflare.R2.ReadWriteBucket(Buste);
    const email = yield* Cloudflare.Email.Send(Email);

    const user = yield* Config.Redacted("ZUCCHETTI_USER");
    const pass = yield* Config.Redacted("ZUCCHETTI_PASS");
    const mailFrom = yield* Config.String("MAIL_FROM");
    const mailTo = yield* Config.String("MAIL_TO");

    return Effect.fn(function* (input: BustaInput | undefined) {
      const event = yield* Cloudflare.Workflows.WorkflowEvent;
      const when = input?.date
        ? new Date(input.date)
        : event.schedule
          ? new Date(event.schedule.scheduledTime)
          : event.timestamp;
      if (event.schedule && oraRoma(when) !== ORA_ROMA) {
        return { skipped: `ore ${oraRoma(when)} a Roma, non ${ORA_ROMA}` };
      }
      const label = zucchettiLabel(when);
      const key = bustaKey(when);

      const body = Effect.gen(function* () {
        const busta = yield* Cloudflare.Workflows.task(
          "scarica-busta",
          Effect.gen(function* () {
            yield* Effect.log(`avvio: cerco la busta ${label}`);
            const browserRun = yield* browser.raw;
            const b = yield* Effect.acquireRelease(launchPuppeteer(browserRun), (b) =>
              Effect.promise(() => b.close()),
            );
            yield* Effect.log("browser: pronto, login su Zucchetti");
            const busta = yield* downloadBusta({
              browser: b,
              user: Redacted.value(user),
              password: Redacted.value(pass),
              year: when.getUTCFullYear(),
              month: when.getUTCMonth() + 1,
            });
            yield* Effect.log(`scaricata: ${busta.label} (${busta.bytes.length} bytes) ${JSON.stringify(busta.source)}`);

            // Il portale a volte serve il documento di un altro mese: una busta
            // byte-identica a quella di un altro mese e' sbagliata, si riprova.
            let cursor: string | undefined;
            do {
              const page = yield* bucket.list({ prefix: "busta-", cursor });
              for (const obj of page.objects) {
                if (obj.key === key || parseBustaKey(obj.key) === null || obj.size !== busta.bytes.length) continue;
                const other = yield* bucket.get(obj.key);
                if (other !== null && sameBytes(new Uint8Array(yield* other.arrayBuffer()), busta.bytes)) {
                  return yield* Effect.fail(
                    new Error(`il portale ha servito per ${label} lo stesso PDF di ${obj.key}: riprovo`),
                  );
                }
              }
              cursor = page.truncated ? page.cursor : undefined;
            } while (cursor !== undefined);
            return busta;
          }).pipe(Effect.mapError(asError)),
          { retries: { limit: 2, delay: "2 minutes", backoff: "exponential" }, timeout: "5 minutes" },
        );

        const { identical } = yield* Cloudflare.Workflows.task(
          "backup-r2",
          Effect.gen(function* () {
            const prev = yield* bucket.get(prevBustaKey(when));
            let identical = false;
            if (prev !== null) {
              const prevArray = new Uint8Array(yield* prev.arrayBuffer());
              identical =
                prevArray.length === busta.bytes.length &&
                prevArray.every((v, i) => v === busta.bytes[i]);
            }
            if (identical) yield* Effect.logWarning("PDF identico al mese precedente");
            yield* bucket.put(key, busta.bytes);
            yield* Effect.log(`backup R2: ${key} (${busta.bytes.length} bytes)`);
            return { identical };
          }).pipe(Effect.mapError(asError)),
          { retries: { limit: 3, delay: "30 seconds", backoff: "exponential" } },
        );

        if (input?.notify !== false) {
          yield* Cloudflare.Workflows.task(
            "email-busta",
            email
              .send({
                from: mailFrom,
                to: mailTo,
                subject: `Busta paga ${label}`,
                text:
                  `${busta.label} in allegato (${busta.bytes.length} bytes). Backup su R2: ${key}.` +
                  (identical ? "\n\nATTENZIONE: il PDF è byte-identico a quello del mese precedente." : ""),
                attachments: [
                  {
                    disposition: "attachment",
                    filename: key,
                    type: "application/pdf",
                    content: busta.bytes,
                  },
                ],
              })
              .pipe(
                Effect.tap(() => Effect.log(`email inviata a ${mailTo} con allegato`)),
                Effect.asVoid,
                Effect.mapError(asError),
              ),
            { retries: { limit: 3, delay: "1 minute", backoff: "exponential" } },
          );
        }

        return { key, bytes: busta.bytes.length, identical };
      });

      return yield* body.pipe(
        Effect.catchCause((cause) =>
          Effect.gen(function* () {
            yield* Cloudflare.Workflows.task(
              "email-errore",
              email
                .send({
                  from: mailFrom,
                  to: mailTo,
                  subject: `Busta paga ${label}: run fallita`,
                  text: `Il workflow ${event.instanceId} è fallito:\n\n${cause}`,
                })
                .pipe(
                  Effect.asVoid,
                  Effect.catchCause((c) => Effect.logWarning(`email di errore non inviata: ${c}`)),
                ),
            );
            return yield* Effect.failCause(cause);
          }),
        ),
      );
    });
  }),
) {}
