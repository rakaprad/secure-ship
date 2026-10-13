package api

import (
	"errors"
	"net/url"
	"strings"
)

// maxURLLength bounds stored URLs; 2048 is a common de facto browser limit.
const maxURLLength = 2048

// validateTarget checks that raw is an absolute http(s) URL that is safe to
// redirect to. selfHost is the shortener's own host; links back to it are
// rejected to prevent redirect loops.
func validateTarget(raw, selfHost string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("url is required")
	}
	if len(raw) > maxURLLength {
		return nil, errors.New("url is too long")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("url is not valid")
	}
	// Only http and https: blocks javascript:, data:, file: and similar
	// schemes that turn an open redirect into XSS or local file access.
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("url scheme must be http or https")
	}
	if u.Hostname() == "" {
		return nil, errors.New("url must include a host")
	}
	// "https://trusted.example@evil.example" is a classic phishing trick.
	if u.User != nil {
		return nil, errors.New("url must not contain credentials")
	}
	if selfHost != "" && strings.EqualFold(u.Hostname(), selfHost) {
		return nil, errors.New("url must not point to this service")
	}
	return u, nil
}

// validCode reports whether code looks like a code this service generates.
// Checking before hitting the store keeps junk input out of lookups and logs.
func validCode(code string) bool {
	if len(code) != codeLength {
		return false
	}
	for _, c := range code {
		if !strings.ContainsRune(alphabet, c) {
			return false
		}
	}
	return true
}
