// Command gonode is the Go-plus-Node hook call the timing probe measures:
// it spawns node once, writes one JSON line to its stdin, reads one line
// back and exits.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

const script = `const rl = require("readline").createInterface({ input: process.stdin });
rl.once("line", (l) => { const m = JSON.parse(l); process.stdout.write(JSON.stringify({ id: m.id, result: "pong" }) + "\n"); process.exit(0); });`

func main() {
	cmd := exec.Command("node", "-e", script)
	stdin, err := cmd.StdinPipe()
	check(err)
	stdout, err := cmd.StdoutPipe()
	check(err)
	cmd.Stderr = os.Stderr
	check(cmd.Start())
	check(json.NewEncoder(stdin).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}))
	line, err := bufio.NewReader(stdout).ReadBytes('\n')
	check(err)
	var reply struct {
		ID     int    `json:"id"`
		Result string `json:"result"`
	}
	check(json.Unmarshal(line, &reply))
	_ = stdin.Close()
	check(cmd.Wait())
	if reply.ID != 1 || reply.Result != "pong" {
		check(fmt.Errorf("unexpected reply %s", line))
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "gonode:", err)
		os.Exit(1)
	}
}
