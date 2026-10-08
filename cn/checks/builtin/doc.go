// Package builtin holds the engine's own checks of the packs folded into
// cn: each is a declared.Builtin tagged with the pack that owns it, which
// declared.LoadSet takes only where that pack is declared, one file per
// check. All returns them; the checks Service hands them to LoadSet.
//
// Each is a port of the Node rule at missingbulb/Claudinite@057841ac,
// keeping its id, on_fail, since, why, doc and finding text:
//
//	shared-tree-immutable       claudinite-lifecycle  packs/claudinite-lifecycle/workRules/shared-tree-immutable.mjs
//	provenance-integrity        claudinite-growth     packs/claudinite-growth/worldRules/provenance-integrity.mjs
//	provenance-change-recorded  claudinite-growth     packs/claudinite-growth/workRules/provenance-change-recorded.mjs
//	routine-structure           claudinite-growth     packs/claudinite-growth/skills/unattended-agents/routine-structure.mjs
//
// and the eight of the task runner:
//
//	task-declaration-shape           claudinite-tasks      packs/claudinite-tasks/worldRules/task-declaration-shape.mjs
//	task-code-work-env               claudinite-tasks      packs/claudinite-tasks/worldRules/task-code-work-env.mjs
//	executor-workflow-secrets        claudinite-tasks      packs/claudinite-tasks/worldRules/executor-workflow-secrets.mjs
//	automerge-policy-scope           claudinite-tasks      packs/claudinite-tasks/workRules/automerge-policy-scope.mjs
//	task-declaration-matches-folder  claudinite-growth     packs/claudinite-growth/worldRules/task-declaration-matches-folder.mjs
//	task-md-only-when-agentic        claudinite-growth     packs/claudinite-growth/worldRules/task-md-only-when-agentic.mjs
//	task-worker-restores-main        claudinite-growth     packs/claudinite-growth/worldRules/task-worker-restores-main.mjs
//	flat-declarations-current        claudinite-lifecycle  packs/claudinite-lifecycle/worldRules/flat-declarations-current.mjs
//
// The folded packs' declared checks are carried as data, not Go: each
// file under declared/ is one pack's declarations, named for its id,
// which LoadSet loads where that pack is declared.
//
// The provenance grammar they read is shared/provenance, from
// engine/checks/helpers/provenance.mjs; the task contract the
// task checks hold declarations to is shared/taskspec, the merge policy
// shared/mergepolicy and the flat files shared/flatdecl.
package builtin
