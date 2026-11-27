import * as Alchemy from "alchemy";
import * as Cloudflare from "@alchemy.run/cloudflare";
import * as Effect from "effect/Effect";
import { apexDomain, fromDomain } from "./src/busta.ts";
import Worker from "./src/worker.ts";

const MAIL_FROM = process.env.MAIL_FROM!;
const MAIL_TO = process.env.MAIL_TO!;

export default Alchemy.Stack(
  "buste-paga",
  { providers: Cloudflare.providers(), state: Cloudflare.state() },
  Effect.gen(function* () {
    const zone = Cloudflare.Zone.Zone.lookup({ name: apexDomain(fromDomain(MAIL_FROM)) });

    const sending = yield* Cloudflare.Email.SendingSubdomain("EmailInvio", {
      zoneId: zone.zoneId,
      name: fromDomain(MAIL_FROM),
    });

    const dest = yield* Cloudflare.Email.Address("EmailDestinazione", { email: MAIL_TO });

    const worker = yield* Worker;
    return {
      url: worker.url,
      sendingSubdomain: sending.name,
      destination: dest.email,
      verified: dest.verified,
    };
  }),
);
