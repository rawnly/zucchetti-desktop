import { test } from "node:test";
import assert from "node:assert/strict";
import {
  bustaKey,
  isBustaKey,
  sameBytes,
  parseBustaKey,
  prevBustaKey,
  timingSafeTokenEqual,
  zucchettiLabel,
  fromDomain,
  apexDomain,
} from "./busta.ts";

test("bustaKey deriva la chiave R2 dal mese corrente", () => {
  assert.equal(bustaKey(new Date("2026-11-27T06:00:00Z")), "busta-2026-11.pdf");
  assert.equal(bustaKey(new Date("2027-01-27T06:00:00Z")), "busta-2027-01.pdf");
});

test("prevBustaKey è il mese precedente (anche a cavallo d'anno)", () => {
  assert.equal(prevBustaKey(new Date("2026-11-27T06:00:00Z")), "busta-2026-10.pdf");
  assert.equal(prevBustaKey(new Date("2027-01-27T06:00:00Z")), "busta-2026-12.pdf");
});

test("zucchettiLabel è la riga del portale MM-YYYY del mese corrente", () => {
  assert.equal(zucchettiLabel(new Date("2026-11-27T06:00:00Z")), "11-2026");
  assert.equal(zucchettiLabel(new Date("2027-01-27T06:00:00Z")), "01-2027");
});

test("fromDomain estrae il dominio dall'indirizzo mittente", () => {
  assert.equal(fromDomain("buste-paga@mail.fedevitale.dev"), "mail.fedevitale.dev");
  assert.equal(fromDomain("noreply@fedevitale.dev"), "fedevitale.dev");
});

test("fromDomain rifiuta indirizzi senza dominio", () => {
  assert.throws(() => fromDomain("senza-chiocciola"));
});

test("apexDomain stacca il primo label: mail.fedevitale.dev → fedevitale.dev", () => {
  assert.equal("fedevitale.dev", apexDomain("mail.fedevitale.dev"));
});

test("apexDomain su un dominio già apex restituisce se stesso", () => {
  assert.equal("example.com", apexDomain("example.com"));
});

test("isBustaKey e parseBustaKey validano le chiavi busta-YYYY-MM.pdf", () => {
  assert.equal(isBustaKey("busta-2026-11.pdf"), true);
  assert.deepEqual(parseBustaKey("busta-2026-11.pdf"), { year: 2026, month: 11 });
  assert.equal(isBustaKey("busta-2026-00.pdf"), false);
  assert.equal(isBustaKey("busta-2026-13.pdf"), false);
  assert.equal(isBustaKey("../secret.pdf"), false);
  assert.equal(parseBustaKey("other.pdf"), null);
});

test("timingSafeTokenEqual confronta token in modo sicuro", async () => {
  assert.equal(await timingSafeTokenEqual("abc123", "abc123"), true);
  assert.equal(await timingSafeTokenEqual("abc123", "abc124"), false);
  assert.equal(await timingSafeTokenEqual(undefined, "abc123"), false);
  assert.equal(await timingSafeTokenEqual("", ""), false);
});

test("sameBytes confronta il contenuto, non solo la lunghezza", () => {
  assert.equal(sameBytes(new Uint8Array([1, 2, 3]), new Uint8Array([1, 2, 3])), true);
  assert.equal(sameBytes(new Uint8Array([1, 2, 3]), new Uint8Array([1, 2, 4])), false);
  assert.equal(sameBytes(new Uint8Array([1, 2]), new Uint8Array([1, 2, 3])), false);
});
