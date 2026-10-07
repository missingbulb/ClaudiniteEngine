package fleet

// Verdict is the per-repo half's whole answer about one repository: where
// it stands, what its tree is, and how fresh it is. `cn fleet judge`
// prints it; the dashboard's fleet view will read the same shape.
type Verdict struct {
	Repo          string     `json:"repo"`
	DefaultBranch string     `json:"defaultBranch"`
	Scope         string     `json:"scope"`
	Shape         Shape      `json:"shape,omitempty"`
	Settings      string     `json:"settings,omitempty"`
	Covered       bool       `json:"covered"`
	Dormant       bool       `json:"dormant"`
	Pin           string     `json:"pin,omitempty"`
	Held          Held       `json:"held,omitempty"`
	HasScheduler  *bool      `json:"hasScheduler,omitempty"`
	Freshness     *Freshness `json:"freshness,omitempty"`
	Error         string     `json:"error,omitempty"`
}

// Held is a member's pack versions by id.
type Held map[string]string

// Judge answers one repository from its reads; pure. A repository out of
// scope, uncovered or dormant is not measured, and readErr says the tree
// could not be read.
func Judge(r Repo, scope string, m Member, readErr error, hasScheduler *bool, in *FreshIn, freshErr error) Verdict {
	v := Verdict{Repo: r.FullName, DefaultBranch: r.Branch(), Scope: scope}
	if scope != ScopeIn {
		return v
	}
	if readErr != nil {
		v.Error = readErr.Error()
		return v
	}
	v.Shape, v.Covered, v.Dormant, v.Settings = m.Shape, m.Covered(), m.Dormant, m.SettingsPath()
	if m.Shape == ShapeCn {
		v.Pin, v.Held = m.Pin.Version, m.Held
	}
	if !v.Covered || v.Dormant {
		return v
	}
	v.HasScheduler = hasScheduler
	switch {
	case freshErr != nil:
		v.Error = freshErr.Error()
	case m.Shape == ShapeNode:
		f := Classify(FreshIn{Shape: ShapeNode})
		v.Freshness = &f
	case in != nil:
		f := Classify(*in)
		v.Freshness = &f
	}
	return v
}

// JudgeRepo reads one repository and judges it: the per-repo half end to
// end.
func JudgeRepo(gh GH, r Repo, home string, cfg Config, shelf Shelf) Verdict {
	scope := Scope(r, home, cfg)
	if scope != ScopeIn {
		return Judge(r, scope, Member{}, nil, nil, nil, nil)
	}
	m, err := ReadMember(gh, r.FullName, r.Branch())
	if err != nil || !m.Covered() || m.Dormant || m.Shape == ShapeNode {
		return Judge(r, scope, m, err, nil, nil, nil)
	}
	has, err := FileExists(gh, r.FullName, SchedulerPath)
	if err != nil {
		return Judge(r, scope, m, nil, nil, nil, err)
	}
	in, err := Measure(m, has, shelf)
	return Judge(r, scope, m, nil, &has, &in, err)
}

// ReadRepo is one repository's listing entry.
func ReadRepo(gh GH, full string) (Repo, error) {
	var r Repo
	resp, err := gh.Get("/repos/" + full)
	if err != nil {
		return r, err
	}
	switch resp.Status {
	case 200:
		if err := jsonUnmarshal(resp.JSON, &r); err != nil {
			return r, err
		}
		return r, nil
	case 403:
		return r, &GrantError{"GET /repos/" + full + " returned 403" + ForbiddenHint("/repos/"+full)}
	}
	return r, &statusError{"GET /repos/" + full, resp.Status}
}
