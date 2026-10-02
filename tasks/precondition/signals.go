package precondition

// Signals are what the collectors read for one verdict, each nil when it
// was not collected and carrying Error when it could not be read. The JSON
// shape is the Node engine's signal bundle.
type Signals struct {
	Runs             *Runs    `json:"runs,omitempty"`
	Commits          *Commits `json:"commits,omitempty"`
	Issues           *Issues  `json:"issues,omitempty"`
	PRs              *PRs     `json:"prs,omitempty"`
	ConversationLogs *Logs    `json:"conversationLogs,omitempty"`
	SharedMount      *Mount   `json:"sharedMount,omitempty"`
	Request          *Request `json:"request,omitempty"`
}

// Runs is the task's own run history, newest first, other than the item
// under evaluation, and the window it was read over.
type Runs struct {
	List   []Run   `json:"list"`
	Window *Window `json:"window,omitempty"`
	Error  string  `json:"error,omitempty"`
}

// Window is a lookback.
type Window struct {
	Days float64 `json:"days"`
}

// Run is one past item of the task.
type Run struct {
	Number    int     `json:"number"`
	CreatedAt *string `json:"createdAt"`
	ClosedAt  *string `json:"closedAt"`
	State     string  `json:"state,omitempty"`
	Status    *string `json:"status,omitempty"`
	Park      *string `json:"park"`
	Outcome   *string `json:"outcome,omitempty"`
}

// Commits is the default branch's movement in the window. A commit
// carrying the task trailer is already classified out of Substantive.
type Commits struct {
	SubstantiveChange bool     `json:"substantiveChange"`
	Count             float64  `json:"count"`
	TouchedPaths      []string `json:"touchedPaths"`
	List              []Commit `json:"list"`
	Error             string   `json:"error,omitempty"`
}

// Commit is one commit in the window.
type Commit struct {
	SHA         string `json:"sha"`
	Substantive bool   `json:"substantive"`
}

// Issues are the repo's open issues and the numbers that moved.
type Issues struct {
	Open    []OpenIssue `json:"open"`
	Touched []int       `json:"touched"`
	Error   string      `json:"error,omitempty"`
}

// OpenIssue is one open issue's number and labels.
type OpenIssue struct {
	Number int      `json:"number"`
	Labels []string `json:"labels"`
}

// PRs are the open pull requests and the numbers that moved.
type PRs struct {
	Open    []OpenPR `json:"open"`
	Touched []int    `json:"touched"`
	Error   string   `json:"error,omitempty"`
}

// OpenPR is one open pull request; ChangedPaths nil means unreadable.
type OpenPR struct {
	Number       int      `json:"number"`
	Title        string   `json:"title"`
	ChangedPaths []string `json:"changedPaths"`
}

// Logs is the conversation-log branch; a nil age is unknown.
type Logs struct {
	NewestLogAgeDays *float64 `json:"newestLogAgeDays"`
	Error            string   `json:"error,omitempty"`
}

// Mount is the vendored canon's movement.
type Mount struct {
	ChangedPacks []string `json:"changedPacks"`
	Error        string   `json:"error,omitempty"`
}

// Request is one marked issue as the request signal reads it.
type Request struct {
	Number           int        `json:"number"`
	Author           string     `json:"author,omitempty"`
	AuthorPermission string     `json:"authorPermission,omitempty"`
	Approvals        []Approval `json:"approvals,omitempty"`
	State            string     `json:"state,omitempty"`
	Queued           bool       `json:"queued,omitempty"`
	Gone             bool       `json:"gone,omitempty"`
	Unreadable       bool       `json:"unreadable,omitempty"`
	Error            string     `json:"error,omitempty"`
}

// Approval is one `/claude go` and its author's permission.
type Approval struct {
	Login      string `json:"login"`
	Permission string `json:"permission"`
}

// state reports whether the named signal was collected and, if it could
// not be read, why.
func (s Signals) state(name string) (present bool, readErr string) {
	switch name {
	case "runs":
		if s.Runs != nil {
			return true, s.Runs.Error
		}
	case "commits":
		if s.Commits != nil {
			return true, s.Commits.Error
		}
	case "issues":
		if s.Issues != nil {
			return true, s.Issues.Error
		}
	case "prs":
		if s.PRs != nil {
			return true, s.PRs.Error
		}
	case "conversationLogs":
		if s.ConversationLogs != nil {
			return true, s.ConversationLogs.Error
		}
	case "sharedMount":
		if s.SharedMount != nil {
			return true, s.SharedMount.Error
		}
	case "request":
		if s.Request != nil {
			return true, s.Request.Error
		}
	}
	return false, ""
}

// Item is the item under evaluation as a term reads it: its number,
// whether somebody created or woke it, and the issue it names.
type Item struct {
	Number  *int `json:"number"`
	Woken   bool `json:"woken"`
	Request *int `json:"request"`
}
