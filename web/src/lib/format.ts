export const GRAPH_AMOUNT_FRACTION_DIGITS = 2;
export const DETAIL_AMOUNT_FRACTION_DIGITS = 4;

export function formatChainAmount(
  raw: string | undefined,
  decimals?: number,
  maxFractionDigits = DETAIL_AMOUNT_FRACTION_DIGITS,
): string {
  if (!raw) return "0";
  const negative = raw.startsWith("-");
  const digits = negative ? raw.slice(1) : raw;
  if (!/^\d+$/.test(digits)) return raw;
  if (decimals === undefined || decimals < 0)
    return digits.length > 12
      ? scientificInteger(digits, negative, 0, maxFractionDigits)
      : raw;
  if (decimals === 0) return raw;
  const visibleDecimals = Math.min(
    decimals,
    Math.max(0, Math.trunc(maxFractionDigits)),
  );
  const scale = decimals - visibleDecimals;
  let rounded = BigInt(digits);
  if (scale > 0) {
    const divisor = 10n ** BigInt(scale);
    const remainder = rounded % divisor;
    rounded /= divisor;
    if (remainder * 2n >= divisor) rounded += 1n;
  }
  const roundedDigits = rounded.toString().padStart(visibleDecimals + 1, "0");
  const roundedWhole = visibleDecimals
    ? roundedDigits.slice(0, -visibleDecimals)
    : roundedDigits;
  const roundedFraction = visibleDecimals
    ? roundedDigits.slice(-visibleDecimals).replace(/0+$/, "")
    : "";
  const groupedWhole = roundedWhole.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  if (rounded === 0n && BigInt(digits) !== 0n)
    return scientificInteger(digits, negative, -decimals, maxFractionDigits);
  return `${negative && rounded !== 0n ? "-" : ""}${groupedWhole}${roundedFraction ? `.${roundedFraction}` : ""}`;
}

export function formatAssetAmount(
  raw: string | undefined,
  decimals: number | undefined,
  symbol: string | undefined,
  maxFractionDigits = DETAIL_AMOUNT_FRACTION_DIGITS,
): string {
  const amount = formatChainAmount(raw, decimals, maxFractionDigits);
  return symbol ? `${amount} ${symbol}` : amount;
}

function scientificInteger(
  digits: string,
  negative: boolean,
  exponentOffset: number,
  maxFractionDigits: number,
): string {
  const significant = digits.replace(/^0+/, "");
  if (!significant) return "0";
  const mantissaDigits = significant.slice(
    0,
    Math.max(1, Math.trunc(maxFractionDigits) + 1),
  );
  const mantissa = `${mantissaDigits[0]}${mantissaDigits.length > 1 ? `.${mantissaDigits.slice(1)}` : ""}`;
  const exponent = significant.length - 1 + exponentOffset;
  return `${negative ? "-" : ""}${mantissa}e${exponent >= 0 ? "+" : ""}${exponent}`;
}

export function displayDecimals(
  assetType: string | undefined,
  asset: string | undefined,
  decimals: number | undefined,
  metadataComplete: boolean | undefined,
): number | undefined {
  if (
    assetType === "eth" ||
    assetType === "native" ||
    asset?.toUpperCase() === "ETH"
  )
    return 18;
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
  return chain === "ethereum" ? "Ethereum" : chain;
}

export function stopReasonLabel(reason?: string): string {
  return (
    (
      {
        high_frequency: "高频交易终点",
        unsupported_contract: "暂不支持的合约终点",
        cross_chain_bridge: "跨链桥终点",
        ambiguous_conversion: "转换证据不足",
        missing_data: "数据缺失，停止追踪",
        no_matching_transfers: "当前覆盖范围无匹配后续",
      } as Record<string, string>
    )[reason ?? ""] ?? "终点"
  );
}
