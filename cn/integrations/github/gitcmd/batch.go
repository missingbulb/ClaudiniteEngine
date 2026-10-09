package gitcmd

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Object is one answer of a batch read: an object's type and content, or
// Missing when the name resolves to nothing.
type Object struct {
	Type    string
	Data    []byte
	Missing bool
}

// Objects reads every named object (a sha, or ref:path) in one git
// process, answers in the order asked. A name holding a newline cannot be
// asked through the batch and answers Missing.
func (r Repo) Objects(names ...string) ([]Object, error) {
	out := make([]Object, len(names))
	var ask []int
	var in strings.Builder
	for i, n := range names {
		if strings.ContainsAny(n, "\n\r") || n == "" {
			out[i].Missing = true
			continue
		}
		ask = append(ask, i)
		in.WriteString(n)
		in.WriteByte('\n')
	}
	if len(ask) == 0 {
		return out, nil
	}
	args := []string{"cat-file", "--batch"}
	if err := r.Faults.spent(args); err != nil {
		return nil, err
	}
	cmd, done := r.child(false, args)
	cmd.Stdin = strings.NewReader(in.String())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err = done(err); err != nil {
		var te *TimeoutError
		if errors.As(err, &te) {
			r.Faults.add(te.msg)
			return nil, te
		}
		return nil, fmt.Errorf("git cat-file --batch: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	br := bufio.NewReader(bytes.NewReader(raw))
	for _, i := range ask {
		head, err := br.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("git cat-file --batch: answer %d of %d is missing", i+1, len(names))
		}
		head = strings.TrimSuffix(head, "\n")
		if strings.HasSuffix(head, " missing") || strings.HasSuffix(head, " ambiguous") {
			out[i].Missing = true
			continue
		}
		f := strings.Fields(head)
		if len(f) != 3 {
			return nil, fmt.Errorf("git cat-file --batch: unexpected answer %q", head)
		}
		size, err := strconv.Atoi(f[2])
		if err != nil {
			return nil, fmt.Errorf("git cat-file --batch: unexpected answer %q", head)
		}
		data := make([]byte, size+1)
		if _, err := io.ReadFull(br, data); err != nil {
			return nil, fmt.Errorf("git cat-file --batch: %s is cut short", f[0])
		}
		out[i] = Object{Type: f[1], Data: data[:size]}
	}
	return out, nil
}

// Files reads each path's blob at ref in one git process, by path; a path
// ref does not hold as a regular blob is absent from the answer.
func (r Repo) Files(ref string, paths []string) (map[string][]byte, error) {
	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = ref + ":" + p
	}
	objs, err := r.Objects(names...)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for i, o := range objs {
		if !o.Missing && o.Type == "blob" {
			out[paths[i]] = o.Data
		}
	}
	return out, nil
}
