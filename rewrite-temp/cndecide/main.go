// Command cndecide answers the parity harness's decision faces that only
// the harness asks: each pure core over a fixture's JSON world, printed as
// JSON, compared with the frozen Node engine's answer. The faces a pack's
// tests also ask (cn tasks <kind> --world) stay in cn.
//
//	cndecide update decide <core> --world FILE
//	cndecide fleet decide <core> --world FILE
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/missingbulb/ClaudiniteEngine/rewrite-temp/cndecide/fleetdecide"
)

var decides = map[string]func(core string, raw []byte) (any, error){
	"update": updateDecide,
	"growth": growthDecide,
	"fleet":  fleetdecide.Decide,
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	answer, rest, err := pick(args)
	if err == nil {
		var raw []byte
		if raw, err = readWorld(rest); err == nil {
			var out any
			if out, err = answer(raw); err == nil {
				err = write(stdout, out)
			}
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "cndecide: %v\n", err)
		return 2
	}
	return 0
}

// pick names the answer args ask for and the flags left to parse.
func pick(args []string) (func([]byte) (any, error), []string, error) {
	if len(args) >= 3 && args[1] == "decide" {
		decide, ok := decides[args[0]]
		if !ok {
			return nil, nil, fmt.Errorf("no %q face", args[0])
		}
		core := args[2]
		return func(raw []byte) (any, error) { return decide(core, raw) }, args[3:], nil
	}
	return nil, nil, fmt.Errorf("usage: cndecide <face> decide <core> --world FILE")
}

func readWorld(args []string) ([]byte, error) {
	fs := flag.NewFlagSet("cndecide", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("world", "", "")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() != 0 || *file == "" {
		return nil, fmt.Errorf("want --world FILE and nothing else")
	}
	return os.ReadFile(*file)
}

func write(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
