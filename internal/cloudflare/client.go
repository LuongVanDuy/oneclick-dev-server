package cloudflare

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/idna"
)

const apiBaseURL = "https://api.cloudflare.com/client/v4"

const maxAPIResponse = 2 * 1024 * 1024

type Client struct {
	token string
	base  string
	http  *http.Client
}

type apiIssue struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type apiEnvelope struct {
	Success bool            `json:"success"`
	Errors  []apiIssue      `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

type APIError struct {
	Status int
	Code   int
	Text   string
}

func (err *APIError) Error() string {
	if err.Code != 0 {
		return fmt.Sprintf("Cloudflare API %d: %s", err.Code, err.Text)
	}
	return fmt.Sprintf("Cloudflare API HTTP %d: %s", err.Status, err.Text)
}

func NewClient(token string) (*Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	return newClient(token, apiBaseURL, &http.Client{Transport: transport, Timeout: 30 * time.Second})
}

func newClient(token, base string, client *http.Client) (*Client, error) {
	token = strings.TrimSpace(token)
	if len(token) < 32 || strings.ContainsAny(token, "\r\n\t ") {
		return nil, errors.New("Cloudflare API token không hợp lệ")
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("Cloudflare API endpoint không hợp lệ")
	}
	if client == nil {
		return nil, errors.New("HTTP client chưa sẵn sàng")
	}
	return &Client{token: token, base: strings.TrimRight(base, "/"), http: client}, nil
}

func (client *Client) request(ctx context.Context, method, path string, body any, output any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.base+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("không kết nối được Cloudflare API: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxAPIResponse+1))
	if err != nil {
		return err
	}
	if len(data) > maxAPIResponse {
		return errors.New("Cloudflare API trả dữ liệu vượt giới hạn")
	}
	var envelope apiEnvelope
	if len(data) > 0 {
		if err := json.Unmarshal(data, &envelope); err != nil {
			return &APIError{Status: response.StatusCode, Text: "phản hồi JSON không hợp lệ"}
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !envelope.Success {
		issue := apiIssue{Message: strings.TrimSpace(response.Status)}
		if len(envelope.Errors) > 0 {
			issue = envelope.Errors[0]
		}
		return &APIError{Status: response.StatusCode, Code: issue.Code, Text: issue.Message}
	}
	if output != nil && len(envelope.Result) > 0 && string(envelope.Result) != "null" {
		if err := json.Unmarshal(envelope.Result, output); err != nil {
			return fmt.Errorf("không đọc được phản hồi Cloudflare API: %w", err)
		}
	}
	return nil
}

func NormalizeZone(value string) (string, error) {
	value = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
	ascii, err := idna.Lookup.ToASCII(value)
	if err != nil || ascii == "" || len(ascii) > 253 || strings.Count(ascii, ".") < 1 {
		return "", errors.New("domain không hợp lệ")
	}
	for _, label := range strings.Split(ascii, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", errors.New("domain không hợp lệ")
		}
	}
	return ascii, nil
}

func NormalizeHostname(value, zone string) (string, error) {
	hostname, err := NormalizeZone(value)
	if err != nil {
		return "", errors.New("subdomain không hợp lệ")
	}
	zone, err = NormalizeZone(zone)
	if err != nil {
		return "", err
	}
	if hostname == zone || !strings.HasSuffix(hostname, "."+zone) {
		return "", fmt.Errorf("subdomain phải nằm trong %s", zone)
	}
	return hostname, nil
}

func (client *Client) VerifyZone(ctx context.Context, value string) (Zone, error) {
	zoneName, err := NormalizeZone(value)
	if err != nil {
		return Zone{}, err
	}
	var tokenStatus struct {
		Status string `json:"status"`
	}
	if err := client.request(ctx, http.MethodGet, "/user/tokens/verify", nil, &tokenStatus); err != nil {
		return Zone{}, fmt.Errorf("API token không dùng được: %w", err)
	}
	if tokenStatus.Status != "active" {
		return Zone{}, errors.New("Cloudflare API token chưa ở trạng thái active")
	}
	query := url.Values{}
	query.Set("name", zoneName)
	query.Set("per_page", "5")
	var zones []struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Status      string   `json:"status"`
		NameServers []string `json:"name_servers"`
		Account     struct {
			ID string `json:"id"`
		} `json:"account"`
	}
	if err := client.request(ctx, http.MethodGet, "/zones?"+query.Encode(), nil, &zones); err != nil {
		return Zone{}, fmt.Errorf("không đọc được domain trên Cloudflare: %w", err)
	}
	for _, item := range zones {
		if strings.EqualFold(item.Name, zoneName) && item.ID != "" && item.Account.ID != "" {
			return Zone{Name: zoneName, ZoneID: item.ID, AccountID: item.Account.ID, Status: item.Status, NameServers: normalizeNames(item.NameServers)}, nil
		}
	}
	return Zone{}, fmt.Errorf("không tìm thấy %s trong tài khoản Cloudflare của token này", zoneName)
}

func LookupNameServers(ctx context.Context, zone string) ([]string, error) {
	zone, err := NormalizeZone(zone)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	type outcome struct {
		source string
		names  []string
		err    error
	}
	resolvers := []struct {
		name     string
		resolver *net.Resolver
	}{
		{name: "system", resolver: net.DefaultResolver},
		{name: "cloudflare", resolver: publicResolver("1.1.1.1")},
		{name: "google", resolver: publicResolver("8.8.8.8")},
		{name: "quad9", resolver: publicResolver("9.9.9.9")},
	}
	completed := make(chan outcome, len(resolvers))
	for _, item := range resolvers {
		go func(source string, resolver *net.Resolver) {
			names, lookupErr := lookupNS(lookupCtx, resolver, zone)
			completed <- outcome{source: source, names: names, err: lookupErr}
		}(item.name, item.resolver)
	}
	var system []string
	public := make([][]string, 0, 3)
	var lastErr error
	for range resolvers {
		select {
		case result := <-completed:
			if result.err != nil {
				lastErr = result.err
				continue
			}
			if result.source == "system" {
				system = result.names
			} else {
				public = append(public, result.names)
			}
		case <-lookupCtx.Done():
			if lastErr == nil {
				lastErr = lookupCtx.Err()
			}
			return chooseNameServerResult(system, public, lastErr)
		}
	}
	return chooseNameServerResult(system, public, lastErr)
}

func lookupNS(ctx context.Context, resolver *net.Resolver, zone string) ([]string, error) {
	items, err := resolver.LookupNS(ctx, zone)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Host)
	}
	return normalizeNames(names), nil
}

func publicResolver(server string) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			protocol := "udp"
			if strings.HasPrefix(network, "tcp") {
				protocol = "tcp"
			}
			dialer := net.Dialer{Timeout: 3 * time.Second}
			return dialer.DialContext(ctx, protocol, net.JoinHostPort(server, "53"))
		},
	}
}

func chooseNameServerResult(system []string, public [][]string, lastErr error) ([]string, error) {
	system = normalizeNames(system)
	counts := map[string]int{}
	groups := map[string][]string{}
	bestKey, bestCount := "", 0
	for _, names := range public {
		names = normalizeNames(names)
		if len(names) == 0 {
			continue
		}
		key := strings.Join(names, "\x00")
		counts[key]++
		groups[key] = names
		if counts[key] > bestCount {
			bestKey, bestCount = key, counts[key]
		}
	}
	// A public website must follow public DNS. Two independent recursive
	// resolvers agreeing is stronger than a stale Windows/ISP resolver cache.
	if bestCount >= 2 {
		return groups[bestKey], nil
	}
	if len(system) > 0 {
		return system, nil
	}
	if bestCount > 0 {
		return groups[bestKey], nil
	}
	if lastErr == nil {
		lastErr = errors.New("không nhận được phản hồi DNS nameserver")
	}
	return nil, lastErr
}

func NameServersMatch(active, assigned []string) bool {
	left, right := normalizeNames(active), normalizeNames(assigned)
	if len(left) == 0 || len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func normalizeNames(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
