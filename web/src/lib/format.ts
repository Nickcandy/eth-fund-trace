export function formatChainAmount(raw: string | undefined, decimals?: number): string {
  if (!raw) return "0";
  if (decimals === undefined || decimals < 0) return raw;
  const negative = raw.startsWith("-");
  const digits = negative ? raw.slice(1) : raw;
  if (!/^\d+$/.test(digits)) return raw;
  if (decimals === 0) return raw;
  const padded = digits.padStart(decimals + 1, "0");
  const whole = padded.slice(0, -decimals);
  const fraction = padded.slice(-decimals).replace(/0+$/, "");
  return `${negative ? "-" : ""}${whole}${fraction ? `.${fraction}` : ""}`;
}

export function shortAddress(address: string, size = 6): string {
  if (address.length <= size * 2 + 2) return address;
  return `${address.slice(0, size + 2)}...${address.slice(-size)}`;
}

export function formatBlock(block: number): string {
  return new Intl.NumberFormat("zh-CN").format(block);
}

export function chainLabel(chain: string): string {
  return chain === "base" ? "Base" : "Ethereum";
}
