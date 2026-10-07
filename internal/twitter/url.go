// Package twitter tags files with the Twitter/X accounts their known URLs
// point at.
package twitter

import (
	"net/url"
	"regexp"
	"strings"
)

// domains are the hosts whose first path segment is the account. Deliberately
// excludes pic.twitter.com and pbs.twimg.com, whose paths are media ids. The
// embed-fixer mirrors keep twitter's own path layout.
var domains = []string{
	"twitter.com", "x.com",
	"fxtwitter.com", "vxtwitter.com", "fixupx.com", "fixvx.com",
}

var subdomains = map[string]bool{"": true, "www": true, "mobile": true, "m": true}

// reserved are site routes that happen to look like handles.
var reserved = map[string]bool{
	"i": true, "home": true, "explore": true, "search": true, "settings": true,
	"intent": true, "hashtag": true, "share": true, "messages": true,
	"notifications": true, "compose": true, "login": true, "logout": true,
	"signup": true, "tos": true, "privacy": true, "about": true,
	"download": true, "account": true, "jobs": true, "lists": true,
	"who_to_follow": true,
}

var handlePattern = regexp.MustCompile(`^[a-z0-9_]{1,15}$`)

// Username returns the account a Twitter/X URL belongs to, lowercased, and
// whether it names one.
//
// Known URLs in the live database are almost all /<user>/status/<id>, with a
// few bare profile links and /i/web/status/<id> links that name nobody.
// Requiring the first path segment to be a syntactically valid handle rejects
// junk without enumerating every site route; the reserved list covers the
// routes that happen to look like handles.
//
// Lowercased because handles are case-insensitive and Hydrus lowercases tags
// on storage anyway; emitting them as stored keeps the push hash stable across
// a URL recorded in two casings.
func Username(rawURL string) (string, bool) {
	u, err := url.Parse(rawURL)
	// url.Parse lowercases the scheme, as .NET's Uri did.
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Opaque != "" {
		return "", false
	}

	if !isTwitterHost(u.Hostname()) {
		return "", false
	}

	path := strings.TrimLeft(u.Path, "/")
	if i := strings.IndexByte(path, '/'); i >= 0 {
		path = path[:i]
	}
	first := strings.ToLower(path)

	if !handlePattern.MatchString(first) || reserved[first] {
		return "", false
	}
	return first, true
}

func isTwitterHost(host string) bool {
	host = strings.ToLower(host)
	for _, domain := range domains {
		if host == domain {
			return true
		}
		if prefix, ok := strings.CutSuffix(host, "."+domain); ok && subdomains[prefix] {
			return true
		}
	}
	return false
}
