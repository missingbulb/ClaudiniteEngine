package update

import (
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/lifecycle/packs"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/rulesindex"
	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/shared/packindex"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// PackBranchPrefix starts every pack update branch; the day follows.
const PackBranchPrefix = "claudinite/packs-"

// PackReader reads signed pack indexes and the archives they name.
type PackReader interface {
	VerifiedIndex(id string) (packs.Verified, error)
	Archive(id string, e packindex.Entry) ([]byte, error)
}

// move is one pack the run moves.
type move struct {
	id, old string
	entry   packindex.Entry
	index   packs.Verified
	archive []byte
}

func describe(moves []move, arrow bool) string {
	var parts []string
	for _, m := range moves {
		if arrow {
			old := m.old
			if old == "" {
				old = "none"
			}
			parts = append(parts, m.id+" "+old+"→"+m.entry.Version)
		} else {
			parts = append(parts, m.id+" "+m.entry.Version)
		}
	}
	return strings.Join(parts, ", ")
}

// botPRs are the open PRs on branches starting with prefix that carry the
// label or that the job token opened, relabelled when the label is gone.
func botPRs(d Deps, prs []githubapi.PR, prefix string) ([]githubapi.PR, error) {
	var out []githubapi.PR
	for _, p := range prs {
		if !strings.HasPrefix(p.HeadRef, prefix) {
			continue
		}
		if !p.HasLabel(Label) {
			if p.Author != gitcmd.BotName {
				continue
			}
			if err := d.GitHub.AddLabel(p.Number, Label); err != nil {
				return nil, err
			}
			p.Labels = append(p.Labels, Label)
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

// Packs is one run of cn update packs. Like Engine it acts at most once:
// a green main is required and an open engine PR makes it wait; a green
// open pack PR is landed; otherwise every declared pack with a newer
// allowed version moves, in one commit on a fresh branch that this repo's
// own check world must pass before it is pushed and proposed.
func Packs(d Deps, o Options) (string, error) {
	head, err := d.Git.Head()
	if err != nil {
		return "", err
	}
	runs, err := d.GitHub.WorkflowRuns(CIWorkflow, head)
	if err != nil {
		return "", err
	}
	if s := runState(latest(runs, "")); s != "success" {
		return "skipped: main is not green (" + s + ")", nil
	}
	if _, skip, err := licenseGate(d); err != nil || skip != "" {
		return skip, err
	}
	all, err := d.GitHub.OpenPulls()
	if err != nil {
		return "", err
	}
	engine, err := botPRs(d, all, BranchPrefix)
	if err != nil {
		return "", err
	}
	if len(engine) > 0 {
		return fmt.Sprintf("skipped: engine PR #%d is open", engine[0].Number), nil
	}
	open, err := botPRs(d, all, PackBranchPrefix)
	if err != nil {
		return "", err
	}
	if len(open) > 1 {
		var names []string
		for _, p := range open {
			names = append(names, "#"+strconv.Itoa(p.Number))
		}
		return "", fmt.Errorf("several open pack PRs (%s): close all but one", strings.Join(names, ", "))
	}
	var prev *githubapi.PR
	prevState := ""
	if len(open) == 1 {
		prev = &open[0]
		runs, err := d.GitHub.WorkflowRuns(CIWorkflow, prev.HeadSHA)
		if err != nil {
			return "", err
		}
		prevState = runState(latest(runs, "workflow_dispatch"))
		if prevState == "success" {
			return Land(d, prev.Number, prev.HeadSHA)
		}
	}

	moves, err := proposePacks(d)
	if err != nil {
		return "", err
	}
	if len(moves) == 0 {
		return "up to date", nil
	}
	set := describe(moves, false)
	if prev != nil && strings.HasSuffix(prev.Title, ": "+describe(moves, true)) {
		why := "its CI concluded " + prevState
		switch prevState {
		case "no run":
			why = "has no CI run"
		case "queued", "in_progress", "waiting", "pending", "requested":
			why = "its CI is " + prevState
		}
		switch prevState {
		case "no run", "cancelled", "timed_out":
			if err := d.GitHub.Dispatch(CIWorkflow, prev.HeadRef, map[string]string{"pr": strconv.Itoa(prev.Number)}); err != nil {
				return "", err
			}
			why += "; dispatched its CI again"
		}
		return fmt.Sprintf("skipped: #%d for packs %s is open and %s", prev.Number, set, why), nil
	}
	for i := range moves {
		data, err := d.Packs.Archive(moves[i].id, moves[i].entry)
		if err != nil {
			return "", err
		}
		moves[i].archive = data
	}
	return openPackPR(d, o, moves, prev)
}

// proposePacks selects, for each declared pack in declared order, the
// version to move to, printing every skip.
func proposePacks(d Deps) ([]move, error) {
	declared, err := packset.Declared(d.Repo)
	if err != nil {
		return nil, err
	}
	path, f, err := settings.Find(d.Repo)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pin, err := settings.ReadEngine(raw, f)
	if err != nil {
		return nil, err
	}
	isDeclared := map[string]bool{}
	for _, id := range declared.Declared {
		isDeclared[id] = true
	}
	var moves []move
	for _, id := range declared.Declared {
		held := ""
		if h, err := packs.HeldVersion(packset.Tree(d.Repo, id)); err == nil {
			held = h
		} else if !errors.Is(err, packset.ErrNoManifest) && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		v, err := d.Packs.VerifiedIndex(id)
		if err != nil {
			return nil, err
		}
		c := packindex.Select(v.Index, packindex.Want{Channel: declared.Channel, Engine: pin.Version, Held: held})
		if c.Skipped != nil {
			fmt.Fprintf(d.Out, "%s %s skipped: %s\n", id, c.Skipped.Version, c.Skipped.Reason)
		}
		if c.Entry == nil {
			continue
		}
		missing := ""
		for _, r := range c.Entry.Requires {
			if !isDeclared[r] {
				missing = r
				break
			}
		}
		if missing != "" {
			fmt.Fprintf(d.Out, "%s %s skipped: requires %s, which is not declared\n", id, c.Entry.Version, missing)
			continue
		}
		moves = append(moves, move{id: id, old: held, entry: *c.Entry, index: v})
	}
	return moves, nil
}

// indexImport is one line the rules index may hold: a vendored or local
// pack's prose, or the copied person's.
var indexImport = regexp.MustCompile(`^@\.\./(shared/packs/[a-z0-9][a-z0-9-]*|local/packs/[A-Za-z0-9][A-Za-z0-9_.-]*)/[A-Za-z0-9_.-]+$|^@\.\./temp/packs/current_user/RULES\.md$`)

// indexShape refuses a rules index holding anything but import lines, so a
// pack PR cannot carry text into every session through it; CI's verify
// judges that the imports are the declaration's.
func indexShape(raw []byte) error {
	text := string(raw)
	if !strings.HasSuffix(text, "\n") {
		return errors.New("does not end in a newline")
	}
	for _, l := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if !indexImport.MatchString(l) {
			return fmt.Errorf("holds %q, which is not a pack's prose import", l)
		}
	}
	return nil
}

// pinVersion is the member's pinned engine version, or 0.0.0 when unreadable;
// the index then loads the packs as a development engine would.
func pinVersion(repo string) string {
	path, f, err := settings.Find(repo)
	if err != nil {
		return "0.0.0"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "0.0.0"
	}
	e, err := settings.ReadEngine(raw, f)
	if err != nil {
		return "0.0.0"
	}
	return e.Version
}

func packsTitle(day int, moves []move) string {
	return fmt.Sprintf("Claudinite packs %d: %s", day, describe(moves, true))
}

// openPackPR writes the moves on a fresh branch, runs this repo's check
// world over it and, when it passes, pushes it, opens and labels the PR
// and dispatches its CI. The checkout ends where it was.
func openPackPR(d Deps, o Options, moves []move, prev *githubapi.PR) (string, error) {
	set := describe(moves, false)
	back, err := d.Git.CurrentBranch()
	if err != nil {
		return "", err
	}
	day := version.Today(d.Now())
	branch := fmt.Sprintf("%s%d", PackBranchPrefix, day)
	title := packsTitle(day, moves)
	if err := d.Git.CreateBranch(branch, "HEAD"); err != nil {
		return "", err
	}
	var rels []string
	err = func() error {
		for _, m := range moves {
			if err := packs.Unpack(m.archive, packset.Tree(d.Repo, m.id)); err != nil {
				return err
			}
			rels = append(rels, packset.TreeRel(m.id))
		}
		changed, err := rulesindex.Write(d.Repo, pinVersion(d.Repo))
		if err != nil {
			return err
		}
		if changed {
			rels = append(rels, rulesindex.File)
		}
		return d.Git.Commit(title, rels...)
	}()
	checkOut, failed := "", false
	if err == nil && !o.Force {
		var errOut string
		var code int
		checkOut, errOut, code, err = child(d.Exe, d.Timeout, "check", "world", "--pr-author", gitcmd.BotName, "--base-ref", mainBranch, "--repo", d.Repo)
		fmt.Fprint(d.Out, checkOut, errOut)
		switch {
		case err != nil:
		case code == 1:
			failed = true
		case code != 0:
			err = fmt.Errorf("check world on %s exited %d", branch, code)
		}
	}
	if err == nil && !failed {
		err = d.Git.Push(remote, branch)
	}
	if cerr := d.Git.Checkout(back); cerr != nil {
		return "", errors.Join(err, cerr)
	}
	if err != nil || failed {
		if derr := d.Git.DeleteBranch(branch); derr != nil {
			err = errors.Join(err, derr)
		}
		if err != nil {
			return "", err
		}
		return "no PR: " + set + " fails this repo's checks", nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Moves this repo's vendored Claudinite packs. Only `%s/` and the rules index `%s` change.\n\n", packset.Dir, rulesindex.File)
	b.WriteString("| Pack | From | To | Channel | Index serial | Source | Key | Archive SHA-256 |\n| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, m := range moves {
		old := m.old
		if old == "" {
			old = "none"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %d | %s | `%s` | `%s` |\n", m.id, old, m.entry.Version, m.entry.Channel, m.index.Index.Serial, m.index.From, m.index.KeyID, m.entry.SHA256)
	}
	b.WriteString("\n")
	if o.Force {
		b.WriteString("**Opened with `--force`:** this repo's `check world` was not run over the branch before it was pushed.\n\n")
	} else if strings.TrimSpace(checkOut) == "" {
		b.WriteString("This repo's `check world` over the branch: no findings.\n\n")
	} else {
		fmt.Fprintf(&b, "This repo's `check world` over the branch:\n\n%s\n%s%s\n\n", fence(checkOut), checkOut, fence(checkOut))
	}
	b.WriteString("The updater merges this PR once the CI run it dispatched is green.\n")

	if prev != nil {
		if err := closeUpdatePR(d, *prev, "Superseded: a newer pack set is proposed ("+set+")."); err != nil {
			return "", err
		}
		if prev.HeadRef == branch {
			if err := d.Git.Push(remote, branch); err != nil {
				return "", err
			}
		}
	}
	pr, err := d.GitHub.CreatePull(title, b.String(), branch, mainBranch)
	if err != nil {
		return "", err
	}
	if err := d.GitHub.AddLabel(pr.Number, Label); err != nil {
		return "", err
	}
	if err := d.GitHub.Dispatch(CIWorkflow, branch, map[string]string{"pr": strconv.Itoa(pr.Number)}); err != nil {
		return "", err
	}
	if err := d.Git.DeleteBranch(branch); err != nil {
		return "", err
	}
	return fmt.Sprintf("opened #%d for packs %s", pr.Number, set), nil
}

// landPacks checks pack PR pr is the updater's own: every changed file
// under the vendored packs or the rules index (import lines only), each touched pack's tree exactly the archive
// its index names at the tree's version, fetched and verified again now,
// and newer than main's. Then it merges.
func landPacks(d Deps, pr githubapi.PR, sha string) (string, error) {
	base := remote + "/" + mainBranch
	files, err := d.Git.ChangedFiles(base, sha)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("#%d changes nothing", pr.Number)
	}
	touched := map[string]bool{}
	var ids []string
	for _, f := range files {
		if f == rulesindex.File {
			idx, ok, err := d.Git.Show(sha, f)
			if err != nil {
				return "", err
			}
			if ok {
				if err := indexShape(idx); err != nil {
					return "", fmt.Errorf("#%d: %s: %w", pr.Number, f, err)
				}
			}
			continue
		}
		rest, ok := strings.CutPrefix(f, packset.Dir+"/")
		id, _, nested := strings.Cut(rest, "/")
		if !ok || !nested {
			return "", fmt.Errorf("#%d changes %s, outside %s/", pr.Number, f, packset.Dir)
		}
		if !touched[id] {
			touched[id] = true
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var landed []string
	for _, id := range ids {
		prefix := packset.TreeRel(id) + "/"
		tree, err := d.Git.Tree(sha, prefix)
		if err != nil {
			return "", err
		}
		have := map[string]packs.File{}
		for p, f := range tree {
			have[strings.TrimPrefix(p, prefix)] = packs.File{Data: f.Data, Executable: f.Executable}
		}
		var mfName string
		for _, n := range packset.ManifestFiles() {
			if _, ok := have[n]; ok {
				if mfName != "" {
					return "", fmt.Errorf("#%d leaves %s with two manifests, %s and %s", pr.Number, packset.TreeRel(id), mfName, n)
				}
				mfName = n
			}
		}
		if mfName == "" {
			return "", fmt.Errorf("#%d leaves %s without a pack manifest", pr.Number, packset.TreeRel(id))
		}
		m, err := packset.ParseManifestFile(mfName, have[mfName].Data)
		if err != nil {
			return "", fmt.Errorf("#%d: %s: %w", pr.Number, id, err)
		}
		if cur, inMain, err := d.Git.Show(base, path.Join(packset.TreeRel(id), mfName)); err != nil {
			return "", err
		} else if inMain {
			if cm, err := packset.ParseManifestFile(mfName, cur); err == nil {
				if c, err := version.ComparePack(m.Version, cm.Version); err != nil || c <= 0 {
					return "", fmt.Errorf("#%d holds %s %s, not newer than main's %s", pr.Number, id, m.Version, cm.Version)
				}
			}
		}
		v, err := d.Packs.VerifiedIndex(id)
		if err != nil {
			return "", err
		}
		e, ok := v.Index.Entry(m.Version)
		if !ok {
			return "", fmt.Errorf("#%d holds %s %s, which its index does not list", pr.Number, id, m.Version)
		}
		if e.Revoked {
			return "", fmt.Errorf("#%d holds %s %s, which is revoked", pr.Number, id, m.Version)
		}
		archive, err := d.Packs.Archive(id, e)
		if err != nil {
			return "", err
		}
		diff, err := packs.FilesEqual(have, archive)
		if err != nil {
			return "", err
		}
		if diff != "" {
			return "", fmt.Errorf("#%d: %s is not the published %s %s:\n%s", pr.Number, packset.TreeRel(id), id, m.Version, diff)
		}
		landed = append(landed, id+" "+m.Version)
	}
	if err := d.GitHub.MergePull(pr.Number, sha, pr.Title); err != nil {
		return "", err
	}
	if err := d.Git.DeleteRemoteBranch(remote, pr.HeadRef); err != nil {
		return "", err
	}
	if err := d.GitHub.Dispatch(CIWorkflow, mainBranch, map[string]string{}); err != nil {
		return "", err
	}
	return "landed packs " + strings.Join(landed, ", "), nil
}
