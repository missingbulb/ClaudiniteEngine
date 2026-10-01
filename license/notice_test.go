package license

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Each notice renders from a fixture key or cause as one sentence telling
// Claude to tell the person, with the link its case wants.
func TestNotices(t *testing.T) {
	grace := minted(t, map[string]any{"state": "grace", "grace_until": testNow.Add(72 * time.Hour).Unix(),
		"seats": map[string]any{"paid": 2, "counted": 5, "headroom": 0}, "checkout_url": "https://polar.sh/c/acme"})
	seat := minted(t, map[string]any{"state": "degraded", "features": []string{}, "checkout_url": "https://polar.sh/c/acme"})
	over := minted(t, map[string]any{"notice": "over-within-headroom", "portal_url": "https://polar.sh/p/acme"})
	unverified := minted(t, map[string]any{"state": "unverified"})
	ok := minted(t, nil)
	for name, c := range map[string]struct {
		got  string
		want []string
	}{
		"app-not-installed": {NoticeFor(nil, CauseAppNotInstalled, "", ""), []string{"not installed", InstallURL, "tell the person"}},
		"no-push-access":    {NoticeFor(nil, CauseNoPushAccess, "", ""), []string{"push access", "tell the person"}},
		"server":            {NoticeFor(nil, CauseServerUnreachable, "", ""), []string{"license server did not answer"}},
		"github":            {NoticeFor(nil, CauseGitHubUnreachable, "", ""), []string{"GitHub did not answer"}},
		"seat-refused":      {NoticeFor(&seat, "", "", ""), []string{"seat was refused", "acme", "https://polar.sh/c/acme"}},
		"grace":             {NoticeFor(&grace, "", "", ""), []string{"grace", "by 3", "2026-10-04", "https://polar.sh/c/acme"}},
		"over":              {NoticeFor(&over, "", "", ""), []string{"within the tolerance", "https://polar.sh/p/acme"}},
		"unverified":        {NoticeFor(&unverified, "", "", ""), []string{"could not verify this session; every feature is on"}},
		"bind-nonce":        {NoticeFor(nil, CauseBindNonce, "a key for another session's nonce", ""), []string{"does not fit", "next session asks again"}},
		"bind-plan":         {NoticeFor(nil, CauseBindPlan, "a public key for acme on a private repo of acme", "https://polar.sh/c/acme"), []string{"does not fit", "public key", "https://polar.sh/c/acme"}},
		"login-expired":     {NoticeFor(nil, CauseLoginExpired, "", ""), []string{"cn login"}},
		"refusal":           {NoticeFor(nil, "workflow-not-pinned", "", ""), []string{"refused the key (workflow-not-pinned)"}},
	} {
		for _, w := range c.want {
			if !strings.Contains(c.got, w) {
				t.Errorf("%s: %q lacks %q", name, c.got, w)
			}
		}
		if strings.Count(strings.TrimSuffix(c.got, "."), ". ") > 0 || strings.Contains(c.got, "\n") {
			t.Errorf("%s: not one sentence: %q", name, c.got)
		}
	}
	if n := NoticeFor(&ok, "", "", ""); n != "" {
		t.Errorf("an ok key has a notice: %q", n)
	}
}

// The App's slug is pinned by AppSlug; with CLAUDINITE_LIVE=1 its public
// page must answer.
func TestAppSlugIsLive(t *testing.T) {
	if os.Getenv("CLAUDINITE_LIVE") != "1" {
		t.Skip("CLAUDINITE_LIVE=1 checks the App's page on github.com")
	}
	resp, err := http.Get("https://github.com/apps/" + AppSlug)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("https://github.com/apps/%s answered %s", AppSlug, resp.Status)
	}
}
