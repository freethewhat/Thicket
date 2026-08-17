// Package update checks for and installs newer thicket releases from
// GitHub Releases.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const releasesURL = "https://api.github.com/repos/freethewhat/Thicket/releases/latest"

// LatestTag fetches the tag_name of the latest GitHub release.
func LatestTag(ctx context.Context) (string, error) {
	return fetchTag(ctx, releasesURL)
}

// fetchTag is LatestTag's implementation with an injectable URL, so tests
// can point it at an httptest.Server instead of the real GitHub API.
//
// It returns an error if the request fails outright (offline, DNS
// failure, ctx deadline exceeded), or if the response status is not 200
// — a non-200 response (e.g. GitHub API rate-limiting, which returns 403
// with a {"message": "..."} body and no tag_name field) is rejected
// before any JSON decoding is attempted, so it can never be mistaken for
// "latest version is empty string".
func fetchTag(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("update: unexpected status %d from %s", resp.StatusCode, url)
	}

	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("update: decoding release response: %w", err)
	}
	if body.TagName == "" {
		return "", fmt.Errorf("update: release response had no tag_name")
	}
	return body.TagName, nil
}

// IsNewer reports whether latest is a newer version than current. Both
// are expected in "vX.Y.Z" form (a leading "v" is optional and stripped
// before parsing). current == "dev" (an unbuilt/local source run) always
// returns false, and either string failing to parse as three
// dot-separated integers also returns false — an unparsable latest is
// never treated as newer.
func IsNewer(current, latest string) bool {
	if current == "dev" {
		return false
	}
	c, ok := parseVersion(current)
	if !ok {
		return false
	}
	l, ok := parseVersion(latest)
	if !ok {
		return false
	}
	for i := range c {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
