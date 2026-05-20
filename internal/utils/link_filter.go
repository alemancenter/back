package utils

import (
	"net/url"
	"regexp"
	"strings"
)

// blockedSegments is a curated list of hostname substrings mapped to harmful categories.
// Matching is case-insensitive against the parsed URL hostname only (not path or query).
var blockedSegments = []string{
	// Gambling
	"bet365", "betway", "888casino", "pokerstars", "bovada", "betsafe",
	"unibet", "williamhill", "ladbrokes", "paddypower", "skybet", "betfair",
	"draftkings", "fanduel", "betmgm", "betonline", "mybookie", "bwin",
	"partypoker", "caesarssports", "pointsbet",
	// Adult
	"pornhub", "xvideos", "xnxx", "redtube", "youporn", "tube8",
	"brazzers", "bangbros", "xhamster", "spankbang", "eporner",
	"tnaflix", "sunporno", "3movs", "4tube",
	// Warez / piracy
	"thepiratebay", "piratebay", "1337x", "rarbg", "rutracker",
	"torrentz2", "eztv", "zooqle", "cpasbien", "0daydown", "scnsrc",
	// Suspicious monetising shorteners (redirect abuse, malware gateways)
	"adf.ly", "bc.vc", "sh.st", "shorte.st", "viralurl.com",
	"5z8.info", "zzb.bz",
}

var blockedSegRe *regexp.Regexp

func init() {
	escaped := make([]string, len(blockedSegments))
	for i, s := range blockedSegments {
		escaped[i] = regexp.QuoteMeta(s)
	}
	blockedSegRe = regexp.MustCompile(`(?i)(` + strings.Join(escaped, "|") + `)`)
}

func isBlockedHost(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return false
	}
	return blockedSegRe.MatchString(u.Hostname())
}

var (
	aOpenTagRe = regexp.MustCompile(`(?is)<a\b([^>]*)>`)
	hrefValRe  = regexp.MustCompile(`(?i)\bhref\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
)

// StripBlockedLinks neutralizes href attributes that point to blocked domains,
// replacing them with "#". Content must be sanitized before calling this.
func StripBlockedLinks(html string) string {
	return aOpenTagRe.ReplaceAllStringFunc(html, func(tag string) string {
		return hrefValRe.ReplaceAllStringFunc(tag, func(attr string) string {
			m := hrefValRe.FindStringSubmatch(attr)
			if m == nil {
				return attr
			}
			// m[1]=double-quoted, m[2]=single-quoted, m[3]=unquoted
			raw := m[1]
			if raw == "" {
				raw = m[2]
			}
			if raw == "" {
				raw = m[3]
			}
			if isBlockedHost(raw) {
				return `href="#"`
			}
			return attr
		})
	})
}
