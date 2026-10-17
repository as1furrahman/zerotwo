package tools

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SSRFValidator inspects URLs and IP addresses to prevent Server-Side Request Forgery.
type SSRFValidator struct {
	AllowLocalhost bool // Only for testing; production default is false
}

// NewSSRFValidator returns a standard SSRF validator that blocks loopback, private, link-local, and cloud metadata.
func NewSSRFValidator() *SSRFValidator {
	return &SSRFValidator{AllowLocalhost: false}
}

// IsBlockedIP returns true if the IP is private, loopback, link-local, cloud metadata, or unspecified.
func (v *SSRFValidator) IsBlockedIP(ip net.IP) (bool, string) {
	if ip == nil {
		return true, "nil IP address"
	}

	// Unwrap IPv4-mapped IPv6 addresses (e.g. ::ffff:127.0.0.1)
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}

	if ip.IsUnspecified() {
		return true, "unspecified address (0.0.0.0 / ::)"
	}

	if ip.IsLoopback() {
		if v.AllowLocalhost {
			return false, ""
		}
		return true, "loopback address (127.0.0.0/8 / ::1)"
	}

	if ip.IsPrivate() {
		return true, "private RFC1918 / RFC4193 address"
	}

	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true, "link-local address (169.254.0.0/16 / fe80::/10)"
	}

	// Cloud metadata addresses (AWS, GCP, Azure, DigitalOcean)
	cloudMetadataIPs := []string{
		"169.254.169.254", // AWS/GCP/Azure/DO
		"100.100.100.200", // Alibaba
	}
	for _, meta := range cloudMetadataIPs {
		if ip.String() == meta {
			return true, fmt.Sprintf("cloud metadata IP address (%s)", meta)
		}
	}

	return false, ""
}

// ValidateURL validates scheme and ensures hostname does not resolve to internal/private/metadata destinations.
func (v *SSRFValidator) ValidateURL(ctx context.Context, rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("forbidden URL scheme %q (only http and https allowed)", u.Scheme)
	}

	hostname := strings.ToLower(u.Hostname())
	if hostname == "" {
		return nil, fmt.Errorf("URL has no hostname")
	}

	// Block known metadata and local hostnames explicitly
	blockedHostnames := []string{
		"metadata.google.internal",
		"metadata",
		"instance-data",
	}
	for _, b := range blockedHostnames {
		if hostname == b || strings.HasSuffix(hostname, "."+b) {
			return nil, fmt.Errorf("forbidden cloud metadata hostname %q", hostname)
		}
	}

	if !v.AllowLocalhost && (hostname == "localhost" || strings.HasSuffix(hostname, ".local") || strings.HasSuffix(hostname, ".internal")) {
		return nil, fmt.Errorf("forbidden local network hostname %q", hostname)
	}

	// If hostname is a literal IP
	if ip := net.ParseIP(hostname); ip != nil {
		if blocked, reason := v.IsBlockedIP(ip); blocked {
			return nil, fmt.Errorf("SSRF protection: IP %s is blocked (%s)", ip, reason)
		}
		return u, nil
	}

	// Resolve hostname to IP addresses
	resolver := net.DefaultResolver
	ips, err := resolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return nil, fmt.Errorf("DNS lookup for %s failed: %w", hostname, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IP addresses found for hostname %s", hostname)
	}

	for _, ipAddr := range ips {
		if blocked, reason := v.IsBlockedIP(ipAddr.IP); blocked {
			return nil, fmt.Errorf("SSRF protection: hostname %s resolves to blocked IP %s (%s)", hostname, ipAddr.IP, reason)
		}
	}

	return u, nil
}

// NewSecureHTTPClient returns an *http.Client with SSRF dialer filtering and redirect re-validation.
func NewSecureHTTPClient(validator *SSRFValidator, timeout time.Duration) *http.Client {
	if validator == nil {
		validator = NewSSRFValidator()
	}

	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			// Validate IP before connecting (prevents DNS rebinding)
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("no IPs for %s", host)
			}

			for _, ip := range ips {
				if blocked, reason := validator.IsBlockedIP(ip.IP); blocked {
					return nil, fmt.Errorf("SSRF protection: blocked connection to %s (%s)", ip.IP, reason)
				}
			}
			dialIP := ips[0].IP

			return dialer.DialContext(ctx, network, net.JoinHostPort(dialIP.String(), port))
		},
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("stopped after 5 redirects")
			}

			// Revalidate redirect destination URL
			if _, err := validator.ValidateURL(req.Context(), req.URL.String()); err != nil {
				return fmt.Errorf("redirect blocked by SSRF policy: %w", err)
			}

			// If redirect host differs from original host, drop sensitive headers!
			if len(via) > 0 && via[0].URL.Host != req.URL.Host {
				req.Header.Del("Authorization")
				req.Header.Del("Cookie")
				req.Header.Del("X-Api-Key")
				req.Header.Del("Proxy-Authorization")
			}

			return nil
		},
	}

	return client
}
