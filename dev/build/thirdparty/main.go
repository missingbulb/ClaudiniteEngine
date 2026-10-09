// Command thirdparty prints the license notices of every third-party Go
// module linked into cn, for the npm packages that ship the binary.
//
//	go run ./dev/build/thirdparty > dev/build/THIRD_PARTY_LICENSES
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type module struct{ path, version, dir string }

var noticeFile = regexp.MustCompile(`(?i)^(licen[cs]e|copying|notice)(\.(md|txt))?$`)

// linked lists the non-standard, non-main modules pkg's build imports.
func linked(root, pkg string) ([]module, error) {
	cmd := exec.Command("go", "list", "-deps", "-f", "{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}", pkg)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w", err)
	}
	seen := map[string]module{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 3 {
			seen[f[0]] = module{f[0], f[1], f[2]}
		}
	}
	mods := make([]module, 0, len(seen))
	for _, m := range seen {
		mods = append(mods, m)
	}
	sort.Slice(mods, func(i, j int) bool { return mods[i].path < mods[j].path })
	return mods, nil
}

// render writes each module's license and notice files whole.
func render(mods []module) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("The cn binaries include the following third-party Go modules, under these terms.\n")
	for _, m := range mods {
		entries, err := os.ReadDir(m.dir)
		if err != nil {
			return nil, err
		}
		var names []string
		for _, e := range entries {
			if !e.IsDir() && noticeFile.MatchString(e.Name()) {
				names = append(names, e.Name())
			}
		}
		if len(names) == 0 {
			return nil, fmt.Errorf("%s: no license file", m.path)
		}
		sort.Strings(names)
		fmt.Fprintf(&b, "\n== %s %s ==\n", m.path, m.version)
		for _, n := range names {
			raw, err := os.ReadFile(filepath.Join(m.dir, n))
			if err != nil {
				return nil, err
			}
			fmt.Fprintf(&b, "\n--- %s ---\n\n%s\n", n, strings.TrimRight(string(raw), "\n"))
		}
	}
	return b.Bytes(), nil
}

func main() {
	mods, err := linked(".", "./cn/cli")
	if err == nil {
		var out []byte
		if out, err = render(mods); err == nil {
			_, err = os.Stdout.Write(out)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "thirdparty:", err)
		os.Exit(1)
	}
}
