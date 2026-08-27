package cloudflare

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var projectIdentity = regexp.MustCompile(`^[a-f0-9]{16,64}$`)
var objectIdentity = regexp.MustCompile(`^[a-f0-9-]{32,64}$`)

type tunnelRecord struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ConfigSrc string `json:"config_src"`
	DeletedAt string `json:"deleted_at"`
}

type dnsRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
	Comment string `json:"comment"`
}

func validateSpec(spec DeploymentSpec) (DeploymentSpec, error) {
	if !projectIdentity.MatchString(spec.ProjectID) {
		return DeploymentSpec{}, errors.New("mã dự án không hợp lệ")
	}
	zoneName, err := NormalizeZone(spec.Zone.Name)
	if err != nil || spec.Zone.ZoneID == "" || spec.Zone.AccountID == "" {
		return DeploymentSpec{}, errors.New("kết nối Cloudflare chưa đầy đủ")
	}
	hostname, err := NormalizeHostname(spec.Hostname, zoneName)
	if err != nil {
		return DeploymentSpec{}, err
	}
	spec.Zone.Name = zoneName
	spec.Hostname = hostname
	if spec.TunnelID != "" && !objectIdentity.MatchString(spec.TunnelID) {
		return DeploymentSpec{}, errors.New("Cloudflare tunnel ID không hợp lệ")
	}
	if spec.DNSRecordID != "" && !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(spec.DNSRecordID) {
		return DeploymentSpec{}, errors.New("Cloudflare DNS record ID không hợp lệ")
	}
	return spec, nil
}

func tunnelName(projectID string) string     { return "oneclick-" + projectID }
func managedComment(projectID string) string { return "OneClick Dev Server project " + projectID }

// originAddress maps a project identity to a stable address inside 127/8.
// Every site gets a distinct Nginx listener, so a connector cannot be routed
// to another site merely by changing the HTTP Host header.
func originAddress(projectID string) string {
	parts := make([]uint64, 3)
	for index := range parts {
		value, _ := strconv.ParseUint(projectID[index*2:index*2+2], 16, 8)
		parts[index] = value%254 + 1
	}
	return fmt.Sprintf("127.%d.%d.%d:8080", parts[0], parts[1], parts[2])
}

func (client *Client) CheckHostname(ctx context.Context, spec DeploymentSpec) error {
	spec, err := validateSpec(spec)
	if err != nil {
		return err
	}
	records, err := client.listDNSByName(ctx, spec.Zone.ZoneID, spec.Hostname)
	if err != nil {
		return err
	}
	for _, record := range records {
		owned := record.Comment == managedComment(spec.ProjectID) || (spec.DNSRecordID != "" && record.ID == spec.DNSRecordID)
		if !owned {
			return fmt.Errorf("subdomain %s đã có bản ghi DNS; OneClick sẽ không ghi đè", spec.Hostname)
		}
	}
	return nil
}

func (client *Client) EnsureDeployment(ctx context.Context, input DeploymentSpec) (Allocation, error) {
	spec, err := validateSpec(input)
	if err != nil {
		return Allocation{}, err
	}
	if err := client.CheckHostname(ctx, spec); err != nil {
		return Allocation{}, err
	}
	allocation := Allocation{}
	tunnel, created, err := client.ensureTunnel(ctx, spec)
	if err != nil {
		return allocation, err
	}
	allocation.TunnelID, allocation.TunnelCreated = tunnel.ID, created
	cleanupOnError := func(cause error) (Allocation, error) {
		_ = client.CleanupDeployment(context.Background(), spec, allocation)
		return Allocation{}, cause
	}
	configuration := map[string]any{"config": map[string]any{"ingress": []map[string]any{
		{"hostname": spec.Hostname, "service": "http://" + originAddress(spec.ProjectID), "originRequest": map[string]any{}},
		{"service": "http_status:404"},
	}}}
	path := fmt.Sprintf("/accounts/%s/cfd_tunnel/%s/configurations", url.PathEscape(spec.Zone.AccountID), url.PathEscape(tunnel.ID))
	if err := client.request(ctx, http.MethodPut, path, configuration, nil); err != nil {
		return cleanupOnError(fmt.Errorf("không cấu hình được hostname cho tunnel: %w", err))
	}
	record, createdDNS, err := client.ensureDNS(ctx, spec, tunnel.ID)
	if err != nil {
		return cleanupOnError(err)
	}
	allocation.DNSRecordID, allocation.DNSCreated = record.ID, createdDNS
	var token string
	path = fmt.Sprintf("/accounts/%s/cfd_tunnel/%s/token", url.PathEscape(spec.Zone.AccountID), url.PathEscape(tunnel.ID))
	if err := client.request(ctx, http.MethodGet, path, nil, &token); err != nil {
		return cleanupOnError(fmt.Errorf("không lấy được connector token: %w", err))
	}
	token = strings.TrimSpace(token)
	if len(token) < 32 || strings.ContainsAny(token, "\r\n\t ") {
		return cleanupOnError(errors.New("Cloudflare trả connector token không hợp lệ"))
	}
	allocation.TunnelToken = token
	return allocation, nil
}

func (client *Client) ensureTunnel(ctx context.Context, spec DeploymentSpec) (tunnelRecord, bool, error) {
	if spec.TunnelID != "" {
		var item tunnelRecord
		path := fmt.Sprintf("/accounts/%s/cfd_tunnel/%s", url.PathEscape(spec.Zone.AccountID), url.PathEscape(spec.TunnelID))
		if err := client.request(ctx, http.MethodGet, path, nil, &item); err == nil && item.ID == spec.TunnelID && item.Name == tunnelName(spec.ProjectID) && item.DeletedAt == "" {
			return item, false, nil
		}
	}
	query := url.Values{}
	query.Set("name", tunnelName(spec.ProjectID))
	query.Set("is_deleted", "false")
	path := fmt.Sprintf("/accounts/%s/cfd_tunnel?%s", url.PathEscape(spec.Zone.AccountID), query.Encode())
	var items []tunnelRecord
	if err := client.request(ctx, http.MethodGet, path, nil, &items); err != nil {
		return tunnelRecord{}, false, fmt.Errorf("không kiểm tra được tunnel cũ: %w", err)
	}
	for _, item := range items {
		if item.Name == tunnelName(spec.ProjectID) && item.DeletedAt == "" {
			if item.ConfigSrc != "cloudflare" {
				return tunnelRecord{}, false, errors.New("tunnel cùng tên không phải loại được OneClick hỗ trợ")
			}
			return item, false, nil
		}
	}
	body := map[string]any{"name": tunnelName(spec.ProjectID), "config_src": "cloudflare"}
	var created tunnelRecord
	path = fmt.Sprintf("/accounts/%s/cfd_tunnel", url.PathEscape(spec.Zone.AccountID))
	if err := client.request(ctx, http.MethodPost, path, body, &created); err != nil {
		return tunnelRecord{}, false, fmt.Errorf("không tạo được Cloudflare tunnel: %w", err)
	}
	if created.ID == "" {
		return tunnelRecord{}, false, errors.New("Cloudflare không trả tunnel ID")
	}
	return created, true, nil
}

func (client *Client) listDNSByName(ctx context.Context, zoneID, hostname string) ([]dnsRecord, error) {
	query := url.Values{}
	query.Set("name", hostname)
	query.Set("per_page", "100")
	path := fmt.Sprintf("/zones/%s/dns_records?%s", url.PathEscape(zoneID), query.Encode())
	var records []dnsRecord
	if err := client.request(ctx, http.MethodGet, path, nil, &records); err != nil {
		return nil, fmt.Errorf("không kiểm tra được DNS subdomain: %w", err)
	}
	return records, nil
}

func (client *Client) ensureDNS(ctx context.Context, spec DeploymentSpec, tunnelID string) (dnsRecord, bool, error) {
	target := tunnelID + ".cfargotunnel.com"
	records, err := client.listDNSByName(ctx, spec.Zone.ZoneID, spec.Hostname)
	if err != nil {
		return dnsRecord{}, false, err
	}
	for _, record := range records {
		owned := record.Comment == managedComment(spec.ProjectID) || (spec.DNSRecordID != "" && record.ID == spec.DNSRecordID)
		if !owned {
			return dnsRecord{}, false, fmt.Errorf("subdomain %s đã có bản ghi DNS; OneClick sẽ không ghi đè", spec.Hostname)
		}
		body := map[string]any{"type": "CNAME", "name": spec.Hostname, "content": target, "proxied": true, "ttl": 1, "comment": managedComment(spec.ProjectID)}
		path := fmt.Sprintf("/zones/%s/dns_records/%s", url.PathEscape(spec.Zone.ZoneID), url.PathEscape(record.ID))
		var updated dnsRecord
		if err := client.request(ctx, http.MethodPut, path, body, &updated); err != nil {
			return dnsRecord{}, false, fmt.Errorf("không cập nhật được DNS subdomain: %w", err)
		}
		return updated, false, nil
	}
	body := map[string]any{"type": "CNAME", "name": spec.Hostname, "content": target, "proxied": true, "ttl": 1, "comment": managedComment(spec.ProjectID)}
	path := fmt.Sprintf("/zones/%s/dns_records", url.PathEscape(spec.Zone.ZoneID))
	var created dnsRecord
	if err := client.request(ctx, http.MethodPost, path, body, &created); err != nil {
		return dnsRecord{}, false, fmt.Errorf("không tạo được DNS subdomain: %w", err)
	}
	return created, true, nil
}

func (client *Client) CleanupDeployment(ctx context.Context, spec DeploymentSpec, allocation Allocation) error {
	var messages []string
	if allocation.DNSCreated && allocation.DNSRecordID != "" {
		path := fmt.Sprintf("/zones/%s/dns_records/%s", url.PathEscape(spec.Zone.ZoneID), url.PathEscape(allocation.DNSRecordID))
		if err := client.request(ctx, http.MethodDelete, path, nil, nil); err != nil && !isNotFound(err) {
			messages = append(messages, err.Error())
		}
	}
	if allocation.TunnelCreated && allocation.TunnelID != "" {
		path := fmt.Sprintf("/accounts/%s/cfd_tunnel/%s", url.PathEscape(spec.Zone.AccountID), url.PathEscape(allocation.TunnelID))
		if err := client.request(ctx, http.MethodDelete, path, nil, nil); err != nil && !isNotFound(err) {
			messages = append(messages, err.Error())
		}
	}
	if len(messages) > 0 {
		return errors.New(strings.Join(messages, "; "))
	}
	return nil
}

func (client *Client) DeleteDeployment(ctx context.Context, input DeploymentSpec) error {
	spec, err := validateSpec(input)
	if err != nil {
		return err
	}
	var messages []string
	if spec.DNSRecordID != "" {
		path := fmt.Sprintf("/zones/%s/dns_records/%s", url.PathEscape(spec.Zone.ZoneID), url.PathEscape(spec.DNSRecordID))
		if err := client.request(ctx, http.MethodDelete, path, nil, nil); err != nil && !isNotFound(err) {
			messages = append(messages, "DNS: "+err.Error())
		}
	} else if records, listErr := client.listDNSByName(ctx, spec.Zone.ZoneID, spec.Hostname); listErr != nil {
		messages = append(messages, "DNS: "+listErr.Error())
	} else {
		for _, record := range records {
			if record.Comment != managedComment(spec.ProjectID) {
				continue
			}
			path := fmt.Sprintf("/zones/%s/dns_records/%s", url.PathEscape(spec.Zone.ZoneID), url.PathEscape(record.ID))
			if err := client.request(ctx, http.MethodDelete, path, nil, nil); err != nil && !isNotFound(err) {
				messages = append(messages, "DNS: "+err.Error())
			}
		}
	}
	if spec.TunnelID != "" {
		path := fmt.Sprintf("/accounts/%s/cfd_tunnel/%s", url.PathEscape(spec.Zone.AccountID), url.PathEscape(spec.TunnelID))
		if err := client.request(ctx, http.MethodDelete, path, nil, nil); err != nil && !isNotFound(err) {
			messages = append(messages, "tunnel: "+err.Error())
		}
	} else {
		query := url.Values{}
		query.Set("name", tunnelName(spec.ProjectID))
		query.Set("is_deleted", "false")
		path := fmt.Sprintf("/accounts/%s/cfd_tunnel?%s", url.PathEscape(spec.Zone.AccountID), query.Encode())
		var tunnels []tunnelRecord
		if err := client.request(ctx, http.MethodGet, path, nil, &tunnels); err != nil {
			messages = append(messages, "tunnel: "+err.Error())
		} else {
			for _, item := range tunnels {
				if item.Name != tunnelName(spec.ProjectID) || item.DeletedAt != "" || item.ConfigSrc != "cloudflare" {
					continue
				}
				path := fmt.Sprintf("/accounts/%s/cfd_tunnel/%s", url.PathEscape(spec.Zone.AccountID), url.PathEscape(item.ID))
				if err := client.request(ctx, http.MethodDelete, path, nil, nil); err != nil && !isNotFound(err) {
					messages = append(messages, "tunnel: "+err.Error())
				}
			}
		}
	}
	if len(messages) > 0 {
		return errors.New(strings.Join(messages, "; "))
	}
	return nil
}

func isNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}
