// The member's FLAT task declarations: every declared pack's task.json, written into
// one file by the member's converge. One read at a sha replaces a read per task, and
// like every content read it is cached under the sha, so a warm load spends nothing
// on it.
//
// A member whose converge predates the flat file has none; the reader here answers
// null for it and the caller falls back to reading the sources one by one. Whether
// the file is there comes from the tree listing the caller already holds, so that
// answer costs no request.
//
// Spelled here, not imported: the page renders other repos in the viewer's browser and
// imports nothing from the engine. The engine's site tests hold the copy to cn's own.
export const FLAT_TASKS_PATH = '.claudinite/cache/tasks.GENERATED.json';

// The directory `cn` wrote these files into before `.claudinite/cache/`: a member whose
// engine update has landed but whose next pack update has not still holds them there.
// @legacy-tolerance advisory:rules-index-current retire:#45
export const LEGACY_FLAT_DIR = '.claudinite/flat/';

// Where this member's listing holds a generated file: the path itself, else its place
// under LEGACY_FLAT_DIR, else null.
export function heldFlatPath(paths, path) {
  const listed = paths ?? [];
  if (listed.includes(path)) return path;
  const legacy = path.replace('.claudinite/cache/', LEGACY_FLAT_DIR);
  return listed.includes(legacy) ? legacy : null;
}

// The flat file's entry map - `{ '<pack>/<task>': { path, declaration | text } }` -
// or null where this member carries no such file, or it could not be read or parsed.
// A read the budget declined throws, as every content read here does, and the caller
// decides what that means.
export async function readFlat({ repo, sha, token, paths, gh }, path, key) {
  const at = heldFlatPath(paths, path);
  if (!at) return null;
  const text = await gh.getTextAtSha(repo, sha, at, token);
  if (!text) return null;
  try {
    const entries = JSON.parse(text)?.[key];
    return entries && typeof entries === 'object' && !Array.isArray(entries) ? entries : null;
  } catch { return null; }
}

// An entry's source as TEXT, which is what every parser on this page takes: the flat
// file keeps the parsed JSON where the source parsed and the raw text where it did not,
// so an unreadable declaration still reads as unreadable.
export const entryText = (entry) => (entry?.declaration !== undefined ? JSON.stringify(entry.declaration) : entry?.text ?? null);

// The flat task entries as the roster's `{ pack, task, path, text }`, kept to the packs
// the declaration names - the same filter the tree walk applies. A local pack's key
// is `local/<name>/<task>`, so the task is what follows the LAST slash.
export function flatTaskRows(entries, declaredPacks) {
  return Object.entries(entries ?? {})
    .map(([key, entry]) => {
      const cut = key.lastIndexOf('/');
      return { pack: key.slice(0, cut), task: key.slice(cut + 1), path: entry?.path ?? null, text: entryText(entry) };
    })
    .filter((row) => declaredPacks.has(row.pack))
    .sort((a, b) => `${a.pack}/${a.task}`.localeCompare(`${b.pack}/${b.task}`));
}
