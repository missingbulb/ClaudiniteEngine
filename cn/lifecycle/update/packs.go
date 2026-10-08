package update

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/packs"
	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/rulesindex"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/flatdecl"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packindex"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/version"
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

func describe(moves []move, arrow bool) string { return describeMoves(packMoves(moves), arrow) }

func packMoves(moves []move) []PackMove {
	out := make([]PackMove, len(moves))
	for i, m := range moves {
		out[i] = PackMove{ID: m.id, From: m.old, To: m.entry.Version}
	}
	return out
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
// own check world must pass before it is pushed and proposed, and lands
// in the same run when its CI passes in time.
func Packs(d Deps, o Options) (string, error) {
	r, err := PacksRun(d, o)
	return r.Verdict, err
}

// PacksRun is Packs, saying whether a CI verdict is still to come: main's
// or the pack PR's.
func PacksRun(d Deps, o Options) (EngineResult, error) {
	var r EngineResult
	v, err := packsRun(d, o, &r)
	if err != nil {
		return EngineResult{}, err
	}
	r.Verdict = v
	return r, nil
}

func packsRun(d Deps, o Options, res *EngineResult) (string, error) {
	if verdict, pending, err := mainGate(d); err != nil || verdict != "" {
		res.MainPending = pending
		return verdict, err
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
		prevState = runState(latest(runs, ciEvents...))
		if prevState == "success" {
			v, err := Land(d, prev.Number, prev.HeadSHA)
			return landed(res, v, err)
		}
	}

	moves, err := proposePacks(d)
	var dis *packs.SourcesDisagree
	if errors.As(err, &dis) {
		return "skipped: " + dis.Error(), nil
	}
	if err != nil {
		return "", err
	}
	if len(moves) == 0 {
		needed, err := indexNeedsPR(d.Repo)
		if err != nil || !needed {
			return "up to date", err
		}
		if prev != nil && prev.Title == IndexTitle {
			return pendingPR(d, res, *prev, prevState, "for the rules index")
		}
		return openPackPR(d, res, o, nil, prev)
	}
	set := describe(moves, false)
	if prev != nil && strings.HasSuffix(prev.Title, ": "+describe(moves, true)) {
		return pendingPR(d, res, *prev, prevState, "for packs "+set)
	}
	for i := range moves {
		data, err := d.Packs.Archive(moves[i].id, moves[i].entry)
		if err != nil {
			return "", err
		}
		moves[i].archive = data
	}
	return openPackPR(d, res, o, moves, prev)
}

// IndexTitle is the title of a pack PR that moves no pack and carries only
// the rules index and the CLAUDE.md import.
const IndexTitle = "claudinite: rules index"

// indexNeedsPR reports whether the member's rules index is stale or
// absent, or CLAUDE.md lacks the import of an index that imports prose,
// or the generated files are still to move out of the legacy directory:
// what a pack PR converges even when no pack moves.
func indexNeedsPR(repo string) (bool, error) {
	if rulesindex.NeedsMove(repo) {
		return true, nil
	}
	st, _, err := rulesindex.Check(repo, pinVersion(repo))
	if err != nil {
		return false, err
	}
	switch st {
	case rulesindex.Empty:
		return false, nil
	case rulesindex.Stale, rulesindex.Absent:
		return true, nil
	}
	return !rulesindex.HasImport(repo), nil
}

// pendingPR is the verdict on the open pack PR proposing what this run
// would: its CI state, asking for its CI again when it never ran or was
// cut short (retryCI).
func pendingPR(d Deps, res *EngineResult, prev githubapi.PR, prevState, what string) (string, error) {
	why := "its CI concluded " + prevState
	switch prevState {
	case "no run":
		why = "has no CI run"
	case "queued", "in_progress", "waiting", "pending", "requested":
		why = "its CI is " + prevState
	}
	tail, err := retryCI(d, res, prev, prevState)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("skipped: #%d %s is open and %s%s", prev.Number, what, why, tail), nil
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

// memberWant is what the member on disk takes: its packs channel and its
// pinned engine.
func memberWant(repo string) (packindex.Want, error) {
	path, f, err := settings.Find(repo)
	if err != nil {
		return packindex.Want{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return packindex.Want{}, err
	}
	pin, err := settings.ReadEngine(raw, f)
	if err != nil {
		return packindex.Want{}, err
	}
	declared, err := settings.ReadPacks(raw, f)
	if err != nil {
		return packindex.Want{}, err
	}
	return packindex.Want{Channel: declared.Channel, Engine: pin.Version}, nil
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

// packsAt writes sha's settings file and pack trees into a fresh
// directory, the tree a derived file on sha is rendered from; the caller
// removes it.
func packsAt(g gitcmd.Repo, sha string) (string, error) {
	tmp, err := os.MkdirTemp("", "claudinite-pack-pr-")
	if err != nil {
		return "", err
	}
	files := map[string]gitcmd.File{}
	for _, f := range settings.Formats {
		rel := settings.RelPath(f)
		data, ok, err := g.Show(sha, rel)
		if err != nil {
			return tmp, err
		}
		if ok {
			files[rel] = gitcmd.File{Data: data}
		}
	}
	for _, root := range []string{packset.Dir, packset.LocalDir} {
		tree, err := g.Tree(sha, root+"/")
		if err != nil {
			return tmp, err
		}
		for p, f := range tree {
			files[p] = f
		}
	}
	for rel, f := range files {
		p := filepath.Join(tmp, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return tmp, err
		}
		if err := os.WriteFile(p, f.Data, 0o644); err != nil {
			return tmp, err
		}
	}
	return tmp, nil
}

// flatRendered refuses a flat file on sha that is not the one the packs
// and the declaration on sha render, so a pack PR carries no other text
// through it.
func flatRendered(g gitcmd.Repo, sha, file string) error {
	tmp, err := packsAt(g, sha)
	defer func() { _ = os.RemoveAll(tmp) }()
	if err != nil {
		return err
	}
	set, err := packset.Load(tmp, pinVersion(tmp), false)
	if err != nil {
		return err
	}
	content, err := flatdecl.Content(tmp, set.Packs)
	if err != nil {
		return err
	}
	have, ok, err := g.Show(sha, file)
	if err != nil {
		return err
	}
	want, rendered := content[flatdecl.Canonical(file)]
	switch {
	case !ok && rendered:
		return errors.New("is removed while the packs render it")
	case ok && (!rendered || string(have) != want):
		return errors.New("is not the file the packs render")
	}
	return nil
}

// skillsIndexRendered refuses a skills index on sha that is not the one
// the packs on sha render, absent where they bundle no skill, so a pack PR
// cannot carry text into every session through it.
func skillsIndexRendered(g gitcmd.Repo, sha string) error {
	tmp, err := packsAt(g, sha)
	defer func() { _ = os.RemoveAll(tmp) }()
	if err != nil {
		return err
	}
	want, err := rulesindex.SkillsContent(tmp, pinVersion(tmp))
	if err != nil {
		return err
	}
	have, ok, err := g.Show(sha, rulesindex.SkillsFile)
	if err != nil {
		return err
	}
	switch {
	case !ok && want != "":
		return errors.New("is removed while the packs bundle skills")
	case ok && want == "":
		return errors.New("is present while no pack bundles a skill")
	case ok && string(have) != want:
		return errors.New("is not the index the packs render")
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

// openPackPR writes the moves on a fresh branch, runs this repo's check
// world over it and, when it passes, pushes it, opens and labels the PR
// and lands it on its CI (awaitLanding). With no moves the branch carries
// the rules index and the import alone. The checkout ends where it was.
func openPackPR(d Deps, res *EngineResult, o Options, moves []move, prev *githubapi.PR) (string, error) {
	set := describe(moves, false)
	what := "packs " + set
	back, err := d.Git.CurrentBranch()
	if err != nil {
		return "", err
	}
	day := version.Today(d.Now())
	branch := fmt.Sprintf("%s%d", PackBranchPrefix, day)
	title := PacksTitle(day, packMoves(moves))
	if len(moves) == 0 {
		set, what, title = "the rules index", "the rules index", IndexTitle
	}
	if err := d.Git.CreateBranch(branch, "HEAD"); err != nil {
		return "", err
	}
	var rels []string
	err = func() error {
		moved, err := rulesindex.Move(d.Repo)
		if err != nil {
			return err
		}
		rels = append(rels, moved...)
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
		if changed, err = rulesindex.WriteSkills(d.Repo, pinVersion(d.Repo)); err != nil {
			return err
		}
		if changed {
			rels = append(rels, rulesindex.SkillsFile)
		}
		flat, err := writeFlat(d.Repo)
		if err != nil {
			return err
		}
		rels = append(rels, flat...)
		if _, err := os.Stat(filepath.Join(d.Repo, filepath.FromSlash(rulesindex.File))); err == nil {
			added, err := rulesindex.EnsureImport(d.Repo)
			if err != nil {
				return err
			}
			if added {
				rels = append(rels, rulesindex.ClaudeMD)
			}
		}
		slices.Sort(rels)
		return d.Git.Commit(title, slices.Compact(rels)...)
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
	fmt.Fprintf(&b, "Moves this repo's vendored Claudinite packs. Only `%s/`, the rules index `%s` and the skills index `%s` change, and `%s` gains the line importing the rules index when it lacks it.\n\n", packset.Dir, rulesindex.File, rulesindex.SkillsFile, rulesindex.ClaudeMD)
	for _, r := range rels {
		if strings.HasPrefix(r, flatdecl.LegacyDir+"/") {
			fmt.Fprintf(&b, "It also moves the generated files out of `%s/` into `%s/`, where this engine writes them, and repoints the import line in `%s`.\n\n", flatdecl.LegacyDir, flatdecl.Dir, rulesindex.ClaudeMD)
			break
		}
	}
	if len(moves) == 0 {
		b.WriteString("No pack moves: the rules index or the import had fallen behind the packs this repo already holds.\n\n")
	} else {
		b.WriteString("| Pack | From | To | Channel | Index serial | Source | Key | Archive SHA-256 |\n| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	}
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
	b.WriteString("The updater approves this PR's held CI run and merges it once that run is green.\n")

	if prev != nil {
		if err := closeUpdatePR(d, *prev, "Superseded: "+set+" is proposed instead."); err != nil {
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
	sha, err := d.Git.RevParse("refs/heads/" + branch)
	if err != nil {
		return "", err
	}
	if err := d.Git.DeleteBranch(branch); err != nil {
		return "", err
	}
	return awaitLanding(d, res, pr.Number, branch, sha, fmt.Sprintf("opened #%d for %s", pr.Number, what))
}

// onlyAppendsImport refuses a CLAUDE.md on sha that is anything but
// base's with the rules index import appended, or with its legacy import
// line repointed, so a pack PR cannot carry other text into every session
// through it.
func onlyAppendsImport(g gitcmd.Repo, base, sha string) error {
	old, had, err := g.Show(base, rulesindex.ClaudeMD)
	if err != nil {
		return err
	}
	cur, has, err := g.Show(sha, rulesindex.ClaudeMD)
	if err != nil {
		return err
	}
	if !has {
		return errors.New("changes more than appending the rules index import")
	}
	repointed := rulesindex.RepointImport(old)
	if had && !bytes.Equal(repointed, old) && bytes.Equal(cur, repointed) {
		return nil
	}
	if (had && rulesindex.HasImportIn(repointed)) || !bytes.Equal(cur, rulesindex.WithImport(repointed)) {
		return errors.New("changes more than appending the rules index import")
	}
	return nil
}

// landPacks checks pack PR pr is the updater's own: every changed file
// under the vendored packs, the rules index (import lines only) or
// CLAUDE.md (the import appended only), the skills index and the flat
// files the PR's packs render, each touched pack's tree exactly the archive
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
		if !IsConvergeBookkeeping(f) {
			return "", fmt.Errorf("#%d changes %s, which no pack update writes", pr.Number, f)
		}
		if strings.HasPrefix(f, flatdecl.LegacyDir+"/") {
			if _, ok, err := d.Git.Show(sha, f); err != nil {
				return "", err
			} else if ok {
				return "", fmt.Errorf("#%d writes %s, under %s/, which a pack update only empties", pr.Number, f, flatdecl.LegacyDir)
			}
			continue
		}
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
		if f == rulesindex.SkillsFile {
			continue
		}
		if isFlatFile(f) {
			if err := flatRendered(d.Git, sha, f); err != nil {
				return "", fmt.Errorf("#%d: %s: %w", pr.Number, f, err)
			}
			continue
		}
		if f == rulesindex.ClaudeMD {
			if err := onlyAppendsImport(d.Git, base, sha); err != nil {
				return "", fmt.Errorf("#%d: %s: %w", pr.Number, f, err)
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
	if err := skillsIndexRendered(d.Git, sha); err != nil {
		return "", fmt.Errorf("#%d: %s: %w", pr.Number, rulesindex.SkillsFile, err)
	}
	sort.Strings(ids)
	want, err := memberWant(d.Repo)
	if err != nil {
		return "", err
	}
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
		// The proposal selected it; landing selects it again, against the
		// member as main holds it, in case the index or the member moved.
		w := want
		if held, err := packs.HeldVersion(packset.Tree(d.Repo, id)); err == nil {
			w.Held = held
		}
		if c := packindex.Select(packindex.Index{Versions: []packindex.Entry{e}}, w); c.Entry == nil {
			why := "it is not newer than what main holds"
			if c.Skipped != nil {
				why = c.Skipped.Reason
			}
			return "", fmt.Errorf("#%d holds %s %s, and this member does not take it: %s", pr.Number, id, m.Version, why)
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
	if err := landPinned(d, pr, sha, pr.Title); err != nil {
		return "", err
	}
	if err := d.GitHub.Dispatch(CIWorkflow, mainBranch, map[string]string{}); err != nil {
		return "", err
	}
	if len(landed) == 0 {
		return "landed the rules index", nil
	}
	return "landed packs " + strings.Join(landed, ", "), nil
}
