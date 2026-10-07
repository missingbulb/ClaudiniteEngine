package main

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/dashdesc"
)

// dashboardDecide is the descriptor-usable check's findings over a JSON
// world of {files: {path: text|null}}, in path order: the dashboard face.
func dashboardDecide(core string, raw []byte) (any, error) {
	if core != "usable" {
		return nil, fmt.Errorf("dashboard decide takes the core usable")
	}
	var in struct {
		Files map[string]*string `json:"files"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(in.Files))
	for p := range in.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	found := dashdesc.Scan(paths, func(p string) (string, bool) {
		t := in.Files[p]
		if t == nil {
			return "", false
		}
		return *t, true
	})
	if found == nil {
		found = []dashdesc.Found{}
	}
	return found, nil
}
