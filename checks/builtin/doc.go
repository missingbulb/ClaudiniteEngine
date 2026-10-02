// Package builtin holds the engine's own checks of the packs folded into
// cn: each is a declared.Builtin tagged with the pack that owns it, which
// declared.LoadSet takes only where that pack is declared, one file per
// check. All returns them; the checks Service hands them to LoadSet.
//
// Each is a port of the Node rule at missingbulb/Claudinite@057841ac,
// keeping its id, on_fail, since, why, doc and finding text:
//
//	shared-tree-immutable       claudinite-lifecycle  packs/claudinite-lifecycle/workRules/shared-tree-immutable.mjs
//	growth-write-scope          claudinite-growth     packs/claudinite-growth/workRules/growth-write-scope.mjs
//	dedup-prune-integrity       claudinite-growth     packs/claudinite-growth/workRules/dedup-integrity.mjs
//	provenance-integrity        claudinite-growth     packs/claudinite-growth/worldRules/provenance-integrity.mjs
//	provenance-change-recorded  claudinite-growth     packs/claudinite-growth/workRules/provenance-change-recorded.mjs
//	routine-structure           claudinite-growth     packs/claudinite-growth/skills/unattended-agents/routine-structure.mjs
//
// The provenance grammar they read is shared/provenance, from
// engine/checks/helpers/provenance.mjs; the growth runs' pinned subjects
// and the dedup fingerprints are shared/growth.
package builtin
