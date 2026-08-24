const MAX_FRACTION_SIGNIFICANT_DIGITS = 6;
const MAX_FRACTION_DISPLAY_PLACES = 12;

export function formatChainAmount(raw: string | undefined, decimals?: number): string {
  if (!raw) return "0";
  const negative = raw.startsWith("-");
  const digits = negative ? raw.slice(1) : raw;
  if (!/^\d+$/.test(digits)) return raw;
  if (decimals === undefined || decimals < 0) return digits.length > 12 ? scientificInteger(digits, negative, 0) : raw;
  if (decimals === 0) return raw;
  const padded = digits.padStart(decimals + 1, "0");
  const whole = padded.slice(0, -decimals);
  const fraction = padded.slice(-decimals);
  const firstNonZero = fraction.search(/[1-9]/);
  const visibleDecimals = whole !== "0"
    ? Math.min(MAX_FRACTION_SIGNIFICANT_DIGITS, decimals)
    : firstNonZero < 0
      ? 0
      : Math.min(decimals, firstNonZero + MAX_FRACTION_SIGNIFICANT_DIGITS, MAX_FRACTION_DISPLAY_PLACES);
  const scale = decimals - visibleDecimals;
  let rounded = BigInt(digits);
  if (scale > 0) {
    const divisor = 10n ** BigInt(scale);
    const remainder = rounded % divisor;
    rounded /= divisor;
    if (remainder * 2n >= divisor) rounded += 1n;
  }
  const roundedDigits = rounded.toString().padStart(visibleDecimals + 1, "0");
  const roundedWhole = visibleDecimals ? roundedDigits.slice(0, -visibleDecimals) : roundedDigits;
  const roundedFraction = visibleDecimals ? roundedDigits.slice(-visibleDecimals).replace(/0+$/, "") : "";
  const groupedWhole = roundedWhole.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  if (rounded === 0n && BigInt(digits) !== 0n) return scientificInteger(digits, negative, -decimals);
  return `${negative && rounded !== 0n ? "-" : ""}${groupedWhole}${roundedFraction ? `.${roundedFraction}` : ""}`;
}

function scientificInteger(digits: string, negative: boolean, exponentOffset: number): string {
  const significant = digits.replace(/^0+/, "");
  if (!significant) return "0";
  const mantissaDigits = significant.slice(0, MAX_FRACTION_SIGNIFICANT_DIGITS);
  const mantissa = `${mantissaDigits[0]}${mantissaDigits.length > 1 ? `.${mantissaDigits.slice(1)}` : ""}`;
  const exponent = significant.length - 1 + exponentOffset;
  return `${negative ? "-" : ""}${mantissa}e${exponent >= 0 ? "+" : ""}${exponent}`;
}

export function displayDecimals(assetType: string | undefined, asset: string | undefined, decimals: number | undefined, metadataComplete: boolean | undefined): number | undefined {
  if (assetType === "eth" || assetType === "native" || asset?.toUpperCase() === "ETH") return 18;
  return metadataComplete === false ? undefined : decimals;
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
