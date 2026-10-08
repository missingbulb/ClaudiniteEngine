// Package config reads the settings file's tasks block: the queue's
// member-side configuration. The block is optional, and each key it
// leaves out takes its default.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/descriptor"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/workitem"
)

// The block's keys.
const (
	// RoutinesKey maps endpoint names to the routines a hand-off fires.
	RoutinesKey = "routines"
	// DeliveryKey says whether an authorized task pull request lands
	// itself or waits for the owner.
	DeliveryKey = "delivery"
	// DisabledKey lists the pack/task ids the scheduler never files.
	DisabledKey = "disabled"
	// DormantKey stops the repo's recurring work while true.
	DormantKey = workitem.DormantConfigKey
)

// The delivery values; absent is AutoMerge.
const (
	AutoMerge = "auto-merge"
	Review    = "review"
)

var schema = descriptor.Schema{Name: "tasks", Keys: map[string]descriptor.Kind{
	RoutinesKey: descriptor.Object, DeliveryKey: descriptor.String,
	DisabledKey: descriptor.List, DormantKey: descriptor.Bool,
}}

// Config is the tasks block with its defaults applied.
type Config struct {
	Routines map[string]any
	Delivery string
	Disabled []string
	Dormant  bool
	// Legacy is whether the settings still declare the retired
	// claudinite-tasks entry.
	Legacy bool
}

// Read reads the repo's tasks block.
func Read(repo string) (Config, error) {
	path, f, err := settings.Find(repo)
	if err != nil {
		return Config{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	p, err := settings.ParseFile(raw, f)
	if err != nil {
		return Config{}, err
	}
	block, legacy := p.TasksBlock()
	c, err := Parse(block)
	c.Legacy = legacy
	return c, err
}

// Parse applies the defaults to a tasks block, nil for none, refusing a
// key it does not know or a value of the wrong shape.
func Parse(block map[string]any) (Config, error) {
	c := Config{Routines: map[string]any{}, Delivery: AutoMerge, Disabled: []string{}}
	if errs := schema.Validate(block); len(errs) > 0 {
		var s []string
		for _, e := range errs {
			s = append(s, e.Error())
		}
		return Config{}, errors.New("tasks: " + strings.Join(s, "; "))
	}
	if r, ok := block[RoutinesKey].(map[string]any); ok {
		c.Routines = r
	}
	if d, ok := block[DeliveryKey].(string); ok {
		if d != AutoMerge && d != Review {
			return Config{}, fmt.Errorf("tasks: %q must be %q or %q, not %q", DeliveryKey, AutoMerge, Review, d)
		}
		c.Delivery = d
	}
	if list, ok := block[DisabledKey].([]any); ok {
		for _, v := range list {
			id, ok := v.(string)
			if !ok {
				return Config{}, fmt.Errorf("tasks: %q must list pack/task ids, not %v", DisabledKey, v)
			}
			c.Disabled = append(c.Disabled, id)
		}
	}
	c.Dormant, _ = block[DormantKey].(bool)
	return c, nil
}
