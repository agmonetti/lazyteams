package graph

import (
	"fmt"
	"net/url"
	"strings"
)

// microsoftHostSuffixes are Microsoft-owned domains. Bearer tokens must never
// be sent to hosts outside this list: a malicious <img> or link inside a
// message could otherwise exfiltrate a live token.
var microsoftHostSuffixes = []string{
	"microsoft.com",
	"cloud.microsoft",
	"office.com",
	"office.net",
	"sharepoint.com",
	"onedrive.com",
	"1drv.ms",
	"skype.com",
	"skypeassets.com",
}

// mediaHostSuffixes extend the Microsoft domains with the CDNs Teams uses to
// serve inline images. These are Akamai buckets controlled by Microsoft.
var mediaHostSuffixes = append(microsoftHostSuffixes, "akamaihd.net")

// isTrustedHost reports whether host is an exact match or a subdomain of one
// of the allowlisted domains.
func isTrustedHost(host string, suffixes []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, suffix := range suffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

// validateURL checks that rawURL is an https URL pointing at a host from the
// given allowlist. Callers attach bearer tokens to their requests, so any URL
// derived from server-controlled data must pass this check first.
func validateURL(rawURL string, suffixes []string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("untrusted URL scheme %q (only https allowed)", u.Scheme)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("URL has no host")
	}
	if !isTrustedHost(u.Hostname(), suffixes) {
		return fmt.Errorf("untrusted host %q", u.Hostname())
	}
	return nil
}

// ValidateMicrosoftURL is the exported variant used by the UI before sending
// bearer tokens to URLs taken from the assignments API.
func ValidateMicrosoftURL(rawURL string) error {
	return validateURL(rawURL, microsoftHostSuffixes)
}

// validateMicrosoftURL is used inside the graph package for endpoints derived
// from server data (Graph content downloads, backward links).
func validateMicrosoftURL(rawURL string) error {
	return validateURL(rawURL, microsoftHostSuffixes)
}

// validateMediaURL is used for AMS image downloads, which may be served from
// Microsoft's image CDNs.
func validateMediaURL(rawURL string) error {
	return validateURL(rawURL, mediaHostSuffixes)
}
