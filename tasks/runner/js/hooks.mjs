// The resolve hook: @claudinite/sdk is the copy the binary shipped, and no
// other @claudinite/* specifier resolves at all, so a pack cannot shadow
// the SDK with its own copy or reach anything else under that scope.
let sdk = null;

export function initialize(data) {
  sdk = data?.sdk ?? null;
}

const SCOPE = '@claudinite/';

export async function resolve(specifier, context, next) {
  if (specifier === '@claudinite/sdk') {
    if (!sdk) throw new Error('@claudinite/sdk: the runner registered no SDK copy');
    return { url: sdk, shortCircuit: true, format: 'module' };
  }
  if (specifier.startsWith(SCOPE)) {
    throw new Error(`${specifier} is refused: @claudinite/sdk is the only module under ${SCOPE} a task script may import`);
  }
  const resolved = await next(specifier, context);
  if (resolved.url.includes('/node_modules/@claudinite/')) {
    throw new Error(`${resolved.url} is refused: a pack may not carry its own copy of anything under ${SCOPE}`);
  }
  return resolved;
}
