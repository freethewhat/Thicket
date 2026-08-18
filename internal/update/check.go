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

// semver holds a parsed "vX.Y.Z" or "vX.Y.Z-pre" version string.
type semver struct {
	major, minor, patch int
	pre                 string // e.g. "beta.1"; empty for a stable release
}

// IsNewer reports whether latest is a newer version than current. Both
// are expected in "vX.Y.Z" or "vX.Y.Z-pre" form (a leading "v" is optional
// and stripped before parsing). current == "dev" (an unbuilt/local source
// run) always returns false, and either string failing to parse also
// returns false — an unparsable latest is never treated as newer.
func IsNewer(current, latest string) bool {
	if current == "dev" {
		return false
	}
	c, ok := parseSemver(current)
	if !ok {
		return false
	}
	l, ok := parseSemver(latest)
	if !ok {
		return false
	}
	return isNewerSemver(c, l)
}

// parseSemver parses "vX.Y.Z" or "vX.Y.Z-pre" (leading "v" optional; pre
// is kept as a raw dot-separated string, not further validated here).
func parseSemver(v string) (semver, bool) {
	var out semver
	v = strings.TrimPrefix(v, "v")
	core := v
	if i := strings.IndexByte(v, '-'); i >= 0 {
		core = v[:i]
		out.pre = v[i+1:]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return out, false
	}
	var nums [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		nums[i] = n
	}
	out.major, out.minor, out.patch = nums[0], nums[1], nums[2]
	return out, true
}

// isNewerSemver reports whether latest has higher precedence than
// current, per semver 2.0.0 §11: the numeric core compares first; for an
// equal core, a release with no prerelease outranks one with a
// prerelease, and two prereleases compare field-by-field via
// comparePrerelease.
func isNewerSemver(current, latest semver) bool {
	if current.major != latest.major {
		return latest.major > current.major
	}
	if current.minor != latest.minor {
		return latest.minor > current.minor
	}
	if current.patch != latest.patch {
		return latest.patch > current.patch
	}
	if current.pre == latest.pre {
		return false
	}
	if current.pre == "" {
		return false // current is already a stable release of this core version
	}
	if latest.pre == "" {
		return true // latest promotes the same core version out of prerelease
	}
	return comparePrerelease(current.pre, latest.pre) < 0
}

// comparePrerelease orders two dot-separated prerelease strings (e.g.
// "beta.1" vs "beta.2") per semver 2.0.0 §11.4: identifiers are compared
// left to right, numeric identifiers as integers (so "9" < "10"), other
// identifiers as strings; if one is a strict prefix of the other, the
// longer one has higher precedence. Returns -1, 0, or 1.
func comparePrerelease(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] == bs[i] {
			continue
		}
		an, aErr := strconv.Atoi(as[i])
		bn, bErr := strconv.Atoi(bs[i])
		if aErr == nil && bErr == nil {
			if an < bn {
				return -1
			}
			return 1
		}
		if as[i] < bs[i] {
			return -1
		}
		return 1
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	default:
		return 0
	}
}
