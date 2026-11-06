package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/desk/view"
	"github.com/doze-dev/doze/internal/dozeversion"
)

// latestURL is doze's newest release, from GitHub; a test points it elsewhere.
var latestURL = "https://api.github.com/repos/doze-dev/doze/releases/latest"

// Update says whether a newer doze is out (Settings › About). It asks only
// when the page asks: on opening About, or Check now; nothing runs in the
// background. Kept six hours, or until force.
func (s *Service) Update(force bool) view.Update {
	s.mu.Lock()
	if !force && s.update != nil && s.now().Sub(s.updateAt) < 6*time.Hour {
		u := *s.update
		s.mu.Unlock()
		return u
	}
	s.mu.Unlock()
	u := view.Update{Current: strings.TrimPrefix(dozeversion.Current, "v"), Dev: !dozeversion.Released(dozeversion.Current)}
	if s.FakeStack != nil {
		return u // a fixture asks nobody
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "doze")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		u.Error = "could not reach GitHub to check"
	} else {
		defer func() { _ = resp.Body.Close() }()
		var rel struct {
			Tag  string `json:"tag_name"`
			URL  string `json:"html_url"`
			Date string `json:"published_at"`
		}
		if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&rel) != nil || rel.Tag == "" {
			u.Error = fmt.Sprintf("GitHub answered %s", resp.Status)
		} else {
			u.Latest, u.URL = strings.TrimPrefix(rel.Tag, "v"), rel.URL
			if t, err := time.Parse(time.RFC3339, rel.Date); err == nil {
				u.Published = t.Local().Format("2 Jan 2006")
			}
			u.Newer = dozeversion.Older(u.Current, u.Latest)
		}
	}
	u.Checked = s.now().Local().Format("15:04")
	s.mu.Lock()
	s.update, s.updateAt = &u, s.now()
	s.mu.Unlock()
	return u
}
