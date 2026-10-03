package adopt

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/interview"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
)

// The interview's writes: an answer the session asked a person for,
// recorded verbatim on the pack's entry. The settings file is the one
// place an answer lives; cn settings answer and init's and adopt's
// --answer are its one writer.

// AnswerFlag is one --answer <pack>/<question>=<text>.
type AnswerFlag struct{ Address, Text string }

// ParseAnswerFlag reads a --answer value.
func ParseAnswerFlag(v string) (AnswerFlag, error) {
	addr, text, ok := strings.Cut(v, "=")
	if !ok {
		return AnswerFlag{}, fmt.Errorf("--answer %q: want <pack>/<question>=<text>", v)
	}
	return AnswerFlag{Address: addr, Text: text}, nil
}

// splitAddress reads <pack>/<question>, where a local pack is
// local/<name>.
func splitAddress(addr string) (string, string, error) {
	i := strings.LastIndex(addr, "/")
	if i <= 0 || i == len(addr)-1 {
		return "", "", fmt.Errorf("%q: want <pack>/<question>", addr)
	}
	return addr[:i], addr[i+1:], nil
}

// Answer records text as the answer to addr on repo's settings file,
// refusing a pack the file does not declare, a question the pack does not
// ask and an empty text ("n/a" is a text). It returns the file written.
func Answer(repo, engine, addr, text string) (string, error) {
	token, question, err := splitAddress(addr)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("%s: the answer is empty; record what the person said, or n/a", addr)
	}
	path, f, err := settings.Find(repo)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	parsed, err := settings.ParseFile(raw, f)
	if err != nil {
		return "", err
	}
	id, local := strings.CutPrefix(token, settings.LocalPrefix)
	if _, ok := parsed.Packs.Entry(id, local); !ok {
		return "", fmt.Errorf("%s does not declare pack %s", settings.RelPath(f), token)
	}
	set, err := packset.Load(repo, engine, false)
	if err != nil {
		return "", err
	}
	var pack *packset.Pack
	for i, p := range set.Packs {
		if p.Token() == token {
			pack = &set.Packs[i]
		}
	}
	if pack == nil {
		for _, nl := range set.NotLoaded {
			if nl.Token == token {
				return "", fmt.Errorf("pack %s did not load (%s), so its questions are unknown", token, nl.Why)
			}
		}
		return "", fmt.Errorf("pack %s is not vendored; run cn adopt or cn update packs first", token)
	}
	var ids []string
	for _, q := range pack.Manifest.Questions {
		if q.ID == question {
			out, err := settings.SetAnswer(raw, f, token, question, text)
			if err != nil {
				return "", err
			}
			return settings.RelPath(f), writeKeepingMode(path, out)
		}
		ids = append(ids, q.ID)
	}
	if len(ids) == 0 {
		return "", fmt.Errorf("pack %s asks no adoption question", token)
	}
	return "", fmt.Errorf("pack %s asks no question %q; it asks %s", token, question, strings.Join(ids, ", "))
}

func writeKeepingMode(path string, raw []byte) error {
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	return os.WriteFile(path, raw, mode)
}

// applyAnswers records each --answer, stopping at the first refusal.
func applyAnswers(repo, engine string, answers []AnswerFlag, out io.Writer) error {
	for _, a := range answers {
		if _, err := Answer(repo, engine, a.Address, a.Text); err != nil {
			return fmt.Errorf("--answer: %w", err)
		}
		fmt.Fprintf(out, "answered %s\n", a.Address)
	}
	return nil
}

// writeQuestions prints the QUESTIONS block, nothing when none is
// pending. Adoption stays incomplete, not failed: the work check gates the
// commit.
func writeQuestions(out io.Writer, pending []interview.Pending) {
	n := interview.Count(pending)
	if n == 0 {
		return
	}
	fmt.Fprintf(out, "\nQUESTIONS — %d adoption question(s) unanswered; ask them in one AskUserQuestion pass and record each with cn settings answer:\n", n)
	for _, p := range pending {
		for _, q := range p.Questions {
			fmt.Fprintf(out, "  %s/%s: %s\n", p.Pack.Token(), q.ID, oneLine(q.Prompt))
			if q.Distill != "" {
				fmt.Fprintf(out, "    distill: %s\n", oneLine(q.Distill))
			}
		}
	}
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
