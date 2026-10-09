// Command cndecide answers the parity harness's decision faces: each pure
// core over a fixture's JSON world, printed as JSON, compared with the
// frozen Node engine's answer. cn itself carries none of these commands.
//
//	cndecide update decide <core> --world FILE
//	cndecide growth decide <core> --world FILE
//	cndecide usage decide fold --world FILE
//	cndecide tasks <kind> --world FILE
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
)

var decides = map[string]func(core string, raw []byte) (any, error){
	"update": updateDecide,
	"growth": growthDecide,
	"usage":  usageDecide,
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	answer, rest, escapeHTML, err := pick(args)
	if err == nil {
		var raw []byte
		if raw, err = readWorld(rest); err == nil {
			var out any
			if out, err = answer(raw); err == nil {
				err = write(stdout, out, escapeHTML)
			}
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "cndecide: %v\n", err)
		return 2
	}
	return 0
}

// pick names the answer args ask for and the flags left to parse. The
// tasks face prints as json.Marshal does, escaping HTML; the others do
// not.
func pick(args []string) (func([]byte) (any, error), []string, bool, error) {
	if len(args) >= 2 && args[0] == "tasks" {
		answer, ok := tasksAnswers[args[1]]
		if !ok {
			return nil, nil, false, fmt.Errorf("unknown tasks kind %q", args[1])
		}
		return answer, args[2:], true, nil
	}
	if len(args) >= 3 && args[1] == "decide" {
		decide, ok := decides[args[0]]
		if !ok {
			return nil, nil, false, fmt.Errorf("no %q face", args[0])
		}
		core := args[2]
		return func(raw []byte) (any, error) { return decide(core, raw) }, args[3:], false, nil
	}
	return nil, nil, false, fmt.Errorf("usage: cndecide <face> decide <core> --world FILE, or cndecide tasks <kind> --world FILE")
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

func write(w io.Writer, v any, escapeHTML bool) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(escapeHTML)
	return enc.Encode(v)
}
