// The order of pack versions, ClaudinitePacks' release index's own: a version not in the
// <major>.<day>.<n> form, such as the two-part 61002.3 the shelf first published, sorts below
// every version in it, and within either form segments compare as numbers, so 61003.10 sorts
// above 61003.9.
const RELEASE = /^(0|[1-9]\d*)\.([1-9]\d*(0[1-9]|1[0-2])|[1-9]|1[0-2])(0[1-9]|[12]\d|3[01])\.[1-9]\d*$/;

export function compareVersions(a, b) {
  const newA = RELEASE.test(a);
  if (newA !== RELEASE.test(b)) return newA ? 1 : -1;
  const x = a.split('.').map(BigInt); const y = b.split('.').map(BigInt);
  for (let i = 0; i < Math.max(x.length, y.length); i += 1) {
    if (i >= x.length) return -1;
    if (i >= y.length) return 1;
    if (x[i] !== y[i]) return x[i] < y[i] ? -1 : 1;
  }
  return 0;
}
