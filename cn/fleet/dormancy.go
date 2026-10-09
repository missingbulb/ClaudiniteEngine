package fleet

// TasksPackID is the pack whose entry carries a scheduler's dormancy.
const TasksPackID = "claudinite-tasks"

// DormantKey is that entry's config key.
const DormantKey = "dormant"

// IsDormant is a Node declaration's dormancy, the predicate the member's
// own Node scheduler stops itself with: the folded packConfig, the
// claudinite-tasks entry's config, then the retired top-level spelling;
// strictly true, so every malformed value reads as awake.
func IsDormant(decl any) bool {
	cfg, ok := decl.(map[string]any)
	if !ok {
		return false
	}
	var v any
	found := false
	if params, ok := packParameters(cfg); ok {
		v, found = params[DormantKey]
	}
	if !found {
		if raw, ok := cfg["raw"].(map[string]any); ok && raw[DormantKey] != nil {
			v = raw[DormantKey]
		} else {
			v = cfg[DormantKey]
		}
	}
	b, ok := v.(bool)
	return ok && b
}

func packParameters(cfg map[string]any) (map[string]any, bool) {
	if pc, ok := cfg["packConfig"].(map[string]any); ok {
		if folded, ok := pc[TasksPackID].(map[string]any); ok {
			return folded, true
		}
	}
	entries, _ := cfg["packs"].([]any)
	for _, e := range entries {
		o, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := o["id"].(string); id == TasksPackID {
			c, ok := o["config"].(map[string]any)
			return c, ok
		}
	}
	return nil, false
}

// EntryDormant is a cn member's dormancy: its tasks block's dormant,
// strictly true.
func EntryDormant(config map[string]any) bool {
	b, ok := config[DormantKey].(bool)
	return ok && b
}
