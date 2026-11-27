const pad2 = (n: number) => String(n).padStart(2, "0");

const BUSTA_KEY_RE = /^busta-(\d{4})-(\d{2})\.pdf$/;

export const bustaKey = (date: Date): string =>
  `busta-${date.getUTCFullYear()}-${pad2(date.getUTCMonth() + 1)}.pdf`;

export const isBustaKey = (key: string): boolean => {
  const m = BUSTA_KEY_RE.exec(key);
  if (!m) return false;
  const month = Number(m[2]);
  return month >= 1 && month <= 12;
};

export const parseBustaKey = (key: string): { year: number; month: number } | null => {
  const m = BUSTA_KEY_RE.exec(key);
  if (!m) return null;
  const year = Number(m[1]);
  const month = Number(m[2]);
  if (month < 1 || month > 12) return null;
  return { year, month };
};

export const timingSafeTokenEqual = async (a: string | undefined, b: string): Promise<boolean> => {
  if (typeof a !== "string" || a.length === 0 || b.length === 0) return false;
  const enc = new TextEncoder();
  const [da, db] = await Promise.all([
    crypto.subtle.digest("SHA-256", enc.encode(a)),
    crypto.subtle.digest("SHA-256", enc.encode(b)),
  ]);
  const ba = new Uint8Array(da);
  const bb = new Uint8Array(db);
  let diff = 0;
  for (let i = 0; i < ba.length; i++) diff |= ba[i] ^ bb[i];
  return diff === 0;
};

export const prevBustaKey = (date: Date): string => {
  const prev = new Date(date);
  prev.setUTCMonth(prev.getUTCMonth() - 1);
  return bustaKey(prev);
};

export const zucchettiLabel = (date: Date): string =>
  `${pad2(date.getUTCMonth() + 1)}-${date.getUTCFullYear()}`;

export const fromDomain = (mailFrom: string): string => {
  const at = mailFrom.lastIndexOf("@");
  if (at < 1 || at === mailFrom.length - 1) {
    throw new Error(`indirizzo mittente non valido: ${mailFrom}`);
  }
  return mailFrom.slice(at + 1);
};

export const apexDomain = (domain: string): string => {
  const labels = domain.split(".");
  if (labels.length < 2) {
    throw new Error(`dominio non valido: ${domain}`);
  }
  return labels.length === 2 ? domain : labels.slice(1).join(".");
};

export const sameBytes = (a: Uint8Array, b: Uint8Array): boolean =>
  a.length === b.length && a.every((v, i) => v === b[i]);
