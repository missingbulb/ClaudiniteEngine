// Command timeit is the timing probe's stopwatch and report writer.
//
//	timeit run --name NAME --runs N --log FILE [--setup SH] [--stdin TEXT] [--exit CODE] -- CMD ARGS...
//	timeit samples --name NAME --log FILE < MILLISECONDS
//	timeit report --log FILE --out DIR --runs N [--title T] [--command C]
//	timeit budget --log FILE --max NAME=MS [--max NAME=MS ...]
//
// run times N executions of CMD (the --setup shell command runs untimed
// before each), each of which must exit CODE (0 by default), and appends
// one JSON line to FILE; samples appends one for times measured elsewhere,
// one per input line; report turns the lines into <platform>-<date>.md and
// .json in DIR, with the host's details; budget fails when a named item's
// median is over its limit, or the log lacks it.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

type item struct {
	Name     string    `json:"name"`
	Runs     int       `json:"runs"`
	MedianMs float64   `json:"medianMs"`
	P95Ms    float64   `json:"p95Ms"`
	MaxMs    float64   `json:"maxMs"`
	Samples  []float64 `json:"samplesMs"`
}

func main() {
	if len(os.Args) < 2 {
		fail(errors.New("usage: timeit run|report"))
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = runCmd(os.Args[2:])
	case "samples":
		err = samplesCmd(os.Args[2:], os.Stdin)
	case "report":
		err = reportCmd(os.Args[2:])
	case "budget":
		err = budgetCmd(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "timeit:", err)
	os.Exit(1)
}

func runCmd(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	name := fs.String("name", "", "")
	runs := fs.Int("runs", 0, "")
	logPath := fs.String("log", "", "")
	setup := fs.String("setup", "", "")
	stdin := fs.String("stdin", "", "")
	want := fs.Int("exit", 0, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || *runs <= 0 || *logPath == "" || fs.NArg() == 0 {
		return errors.New("run needs --name, --runs, --log and a command")
	}
	var samples []float64
	for i := 0; i < *runs; i++ {
		if *setup != "" {
			if out, err := exec.Command("sh", "-c", *setup).CombinedOutput(); err != nil {
				return fmt.Errorf("%s: setup: %v\n%s", *name, err, out)
			}
		}
		cmd := exec.Command(fs.Arg(0), fs.Args()[1:]...)
		cmd.Stdin = strings.NewReader(*stdin)
		start := time.Now()
		out, err := cmd.CombinedOutput()
		elapsed := time.Since(start)
		var exit *exec.ExitError
		code := 0
		if errors.As(err, &exit) {
			code, err = exit.ExitCode(), nil
		}
		if err != nil || code != *want {
			return fmt.Errorf("%s: exit %d, want %d %v\n%s", *name, code, *want, err, out)
		}
		samples = append(samples, float64(elapsed.Microseconds())/1000)
	}
	return appendItem(*logPath, summarize(*name, samples))
}

func samplesCmd(args []string, in io.Reader) error {
	fs := flag.NewFlagSet("samples", flag.ContinueOnError)
	name := fs.String("name", "", "")
	logPath := fs.String("log", "", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || *logPath == "" {
		return errors.New("samples needs --name and --log")
	}
	var samples []float64
	s := bufio.NewScanner(in)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		v, err := strconv.ParseFloat(line, 64)
		if err != nil {
			return fmt.Errorf("%s: %q is not a number of milliseconds", *name, line)
		}
		samples = append(samples, v)
	}
	if err := s.Err(); err != nil {
		return err
	}
	if len(samples) == 0 {
		return fmt.Errorf("%s: no samples", *name)
	}
	return appendItem(*logPath, summarize(*name, samples))
}

type limits []string

func (l *limits) String() string     { return strings.Join(*l, ",") }
func (l *limits) Set(v string) error { *l = append(*l, v); return nil }

func budgetCmd(args []string) error {
	fs := flag.NewFlagSet("budget", flag.ContinueOnError)
	logPath := fs.String("log", "", "")
	var max limits
	fs.Var(&max, "max", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	items, err := readItems(*logPath)
	if err != nil {
		return err
	}
	median := map[string]float64{}
	for _, it := range items {
		median[it.Name] = it.MedianMs
	}
	var over []string
	for _, m := range max {
		name, ms, ok := strings.Cut(m, "=")
		limit, err := strconv.ParseFloat(ms, 64)
		if !ok || err != nil {
			return fmt.Errorf("--max %q is not NAME=MS", m)
		}
		got, ok := median[name]
		switch {
		case !ok:
			over = append(over, fmt.Sprintf("%s: not measured", name))
		case got > limit:
			over = append(over, fmt.Sprintf("%s: median %.1f ms, budget %.1f ms", name, got, limit))
		}
	}
	if len(over) > 0 {
		return fmt.Errorf("over budget: %s", strings.Join(over, "; "))
	}
	return nil
}

func readItems(logPath string) ([]item, error) {
	raw, err := os.ReadFile(logPath)
	if err != nil {
		return nil, err
	}
	var items []item
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	for dec.More() {
		var it item
		if err := dec.Decode(&it); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, nil
}

func appendItem(logPath string, it item) error {
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return json.NewEncoder(f).Encode(it)
}

func summarize(name string, samples []float64) item {
	sorted := append([]float64{}, samples...)
	sort.Float64s(sorted)
	n := len(sorted)
	median := sorted[n/2]
	if n%2 == 0 {
		median = (sorted[n/2-1] + sorted[n/2]) / 2
	}
	p95 := sorted[int(math.Ceil(0.95*float64(n)))-1]
	return item{Name: name, Runs: n, MedianMs: round(median), P95Ms: round(p95), MaxMs: round(sorted[n-1]), Samples: samples}
}

func round(v float64) float64 { return math.Round(v*10) / 10 }

func output(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return "unavailable"
	}
	return strings.TrimSpace(string(out))
}

func cpuModel() string {
	switch runtime.GOOS {
	case "linux":
		f, err := os.Open("/proc/cpuinfo")
		if err != nil {
			return "unavailable"
		}
		defer func() { _ = f.Close() }()
		s := bufio.NewScanner(f)
		for s.Scan() {
			if k, v, ok := strings.Cut(s.Text(), ":"); ok && strings.TrimSpace(k) == "model name" {
				return strings.TrimSpace(v)
			}
		}
	case "darwin":
		return output("sysctl", "-n", "machdep.cpu.brand_string")
	case "windows":
		if v := output("powershell", "-NoProfile", "-Command", "(Get-CimInstance Win32_Processor).Name"); v != "unavailable" {
			return v
		}
		return os.Getenv("PROCESSOR_IDENTIFIER")
	}
	return "unavailable"
}

func reportCmd(args []string) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	logPath := fs.String("log", "", "")
	outDir := fs.String("out", "", "")
	runs := fs.Int("runs", 0, "")
	title := fs.String("title", "Desktop timings", "")
	command := fs.String("command", "", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *command == "" {
		*command = fmt.Sprintf("sh probe/desktop-timings/run.sh --runs %d", *runs)
	}
	items, err := readItems(*logPath)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	host := map[string]string{
		"uname":     output("uname", "-a"),
		"goVersion": output("go", "version"),
		"node":      output("node", "--version"),
		"cpu":       cpuModel(),
	}
	platform := version.Platform()
	date := now.Format("2006-01-02")
	base := filepath.Join(*outDir, platform+"-"+date)
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	res := map[string]any{"platform": platform, "date": date, "runs": *runs, "host": host, "items": items}
	js, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(base+".json", append(js, '\n'), 0o644); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s: %s, %s\n\nProduced by `%s`. Times are wall-clock milliseconds.\n\n", *title, platform, date, *command)
	b.WriteString("| Item | Runs | Median | p95 | Max |\n| --- | ---: | ---: | ---: | ---: |\n")
	for _, it := range items {
		fmt.Fprintf(&b, "| %s | %d | %.1f ms | %.1f ms | %.1f ms |\n", it.Name, it.Runs, it.MedianMs, it.P95Ms, it.MaxMs)
	}
	fmt.Fprintf(&b, "\nHost:\n\n- `uname -a`: %s\n- `go version`: %s\n- `node --version`: %s\n- CPU: %s\n", host["uname"], host["goVersion"], host["node"], host["cpu"])
	return os.WriteFile(base+".md", []byte(b.String()), 0o644)
}
