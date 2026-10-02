package provenance

// At is a fact's place: a file and a 1-based line, 0 for the whole file.
type At struct {
	File string
	Line int
}

// Dangling is a marker or id naming no live file.
type Dangling struct {
	At
	ID, Carrier string
	// Retired is a file whose last entry retired the element.
	Retired bool
}

// Fault is a grammar fault of a provenance file.
type Fault struct {
	At
	What string
}

// Audit is everything an integrity check judges about one pack, as
// facts; the caller writes the finding text.
type Audit struct {
	PackDir  string
	Carriers Carriers
	Files    []File
	// Unmarked are RULES.md rules with no marker.
	Unmarked []Rule
	Dangling []Dangling
	// Unnamed are live files no carrier names.
	Unnamed []File
	// NoBody are skills declaring no body.
	NoBody []Skill
	// MarkerInWorkflow are a workflow skill's bullets ending with a
	// marker.
	MarkerInWorkflow         []Rule
	ParseErrors, EntryFaults []Fault
	// Empty and ConvertedOnly are pending history.
	Empty, ConvertedOnly []File
}

// AuditPack judges the pack at packDir.
func AuditPack(packDir string, io IO) Audit {
	c := PackCarriers(packDir, io)
	files := Files(packDir, io)
	byID := FileMap(files)
	a := Audit{PackDir: packDir, Carriers: c, Files: files}
	named := map[string]bool{}
	name := func(id, carrier string, at At) {
		named[id] = true
		f, ok := byID[id]
		if !ok || f.Status != "live" {
			a.Dangling = append(a.Dangling, Dangling{At: at, ID: id, Carrier: carrier, Retired: ok})
		}
	}
	for _, r := range c.Rules {
		if r.Slug == "" {
			a.Unmarked = append(a.Unmarked, r)
		} else {
			name(r.Slug, `rule "`+r.Trigger+`"`, At{r.File, r.LastLine})
		}
	}
	for _, g := range c.Guidelines {
		if g.Slug != "" {
			name(g.Slug, `guideline "`+g.Trigger+`"`, At{g.File, g.LastLine})
		}
	}
	for _, s := range c.Skills {
		if !s.Present {
			continue
		}
		if s.Body == "" {
			a.NoBody = append(a.NoBody, s)
		}
		if s.Body == "workflow" {
			for _, b := range s.Bullets {
				if b.Slug != "" {
					a.MarkerInWorkflow = append(a.MarkerInWorkflow, b)
				}
			}
		}
		name(s.Name, "skill "+s.Name, At{s.File, 0})
	}
	for _, ch := range c.Checks {
		name(ElementID(ch.ID), "check "+ch.ID, At{ch.File, 0})
	}
	for _, t := range c.Tasks {
		name(t.ID, "task "+t.ID, At{t.File, 0})
	}
	for _, d := range c.Decls {
		name(d.ID, "declared rule "+d.ID, At{d.File, 0})
	}
	if c.ManifestFile != "" {
		name(PackElement, "the manifest", At{packDir + "/" + c.ManifestFile, 0})
	}
	for _, f := range files {
		for _, p := range f.Problems {
			a.ParseErrors = append(a.ParseErrors, Fault{At{f.Path, p.Line}, p.What})
		}
		for _, p := range EntryFaults(f.Entries) {
			a.EntryFaults = append(a.EntryFaults, Fault{At{f.Path, p.Line}, p.What})
		}
		if f.Empty {
			a.Empty = append(a.Empty, f)
		}
		if f.ConvertedOnly {
			a.ConvertedOnly = append(a.ConvertedOnly, f)
		}
		if f.Status == "live" && !named[f.ID] {
			a.Unnamed = append(a.Unnamed, f)
		}
	}
	declined := packDir + "/" + Dir + "/" + DeclinedFile
	if text, ok := io.Read(declined); ok && io.Exists(declined) {
		_, problems := ParseKinds(text, []string{DeclinedKind})
		for _, p := range problems {
			a.ParseErrors = append(a.ParseErrors, Fault{At{declined, p.Line}, p.What})
		}
	}
	return a
}
