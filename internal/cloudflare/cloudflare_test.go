package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNormalizeHostnameKeepsDeploymentInsideZone(t *testing.T) {
	hostname, err := NormalizeHostname("  Demo.KidGrow.Site. ", "kidgrow.site")
	if err != nil || hostname != "demo.kidgrow.site" {
		t.Fatalf("unexpected normalized hostname %q: %v", hostname, err)
	}
	for _, value := range []string{"kidgrow.site", "other.site", "kidgrow.site.evil.test", "bad_name.kidgrow.site"} {
		if _, err := NormalizeHostname(value, "kidgrow.site"); err == nil {
			t.Fatalf("unsafe hostname was accepted: %q", value)
		}
	}
}

func TestPublicDNSConsensusWinsOverStaleWindowsCache(t *testing.T) {
	zoneDNS := []string{"ns1.zonedns.vn", "ns2.zonedns.vn", "ns3.zonedns.vn", "ns4.zonedns.vn"}
	cloudflareNS := []string{"lynn.ns.cloudflare.com", "tricia.ns.cloudflare.com"}
	result, err := chooseNameServerResult(zoneDNS, [][]string{
		cloudflareNS,
		{"tricia.ns.cloudflare.com.", "lynn.ns.cloudflare.com."},
		cloudflareNS,
	}, nil)
	if err != nil || !NameServersMatch(result, cloudflareNS) {
		t.Fatalf("public consensus was not selected: %v / %v", result, err)
	}
}

func TestSystemDNSRemainsFallbackWithoutPublicConsensus(t *testing.T) {
	system := []string{"ns1.example.test", "ns2.example.test"}
	result, err := chooseNameServerResult(system, [][]string{
		{"one.ns.cloudflare.com", "two.ns.cloudflare.com"},
		{"ns1.other.test", "ns2.other.test"},
	}, nil)
	if err != nil || !NameServersMatch(result, system) {
		t.Fatalf("system fallback was not preserved: %v / %v", result, err)
	}
}

func TestLiveKidgrowNameServerConsensus(t *testing.T) {
	if os.Getenv("ONECLICK_LIVE_DNS") != "1" {
		t.Skip("set ONECLICK_LIVE_DNS=1 for the public resolver check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := LookupNameServers(ctx, "kidgrow.site")
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"lynn.ns.cloudflare.com", "tricia.ns.cloudflare.com"}
	if !NameServersMatch(result, expected) {
		t.Fatalf("unexpected public nameservers: %v", result)
	}
}

func TestCheckHostnameNeverOverwritesUnrelatedDNS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+strings.Repeat("a", 32) {
			t.Error("missing bearer token")
		}
		writeEnvelope(t, writer, http.StatusOK, []dnsRecord{{
			ID: strings.Repeat("b", 32), Type: "A", Name: "demo.kidgrow.site", Content: "203.0.113.8",
		}})
	}))
	defer server.Close()
	client, err := newClient(strings.Repeat("a", 32), server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	err = client.CheckHostname(context.Background(), testDeploymentSpec())
	if err == nil || !strings.Contains(err.Error(), "không ghi đè") {
		t.Fatalf("expected safe DNS conflict, got %v", err)
	}
}

func TestEnsureDeploymentDoesNotDeletePreexistingResourcesOnTokenFailure(t *testing.T) {
	var mu sync.Mutex
	deletes := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		if request.Method == http.MethodDelete {
			deletes++
		}
		mu.Unlock()
		switch {
		case request.Method == http.MethodGet && strings.Contains(request.URL.Path, "/dns_records"):
			writeEnvelope(t, writer, http.StatusOK, []dnsRecord{{
				ID: strings.Repeat("d", 32), Type: "CNAME", Name: "demo.kidgrow.site",
				Content: "existing.cfargotunnel.com", Proxied: true, Comment: managedComment("1234567890abcdef"),
			}})
		case request.Method == http.MethodGet && request.URL.Path == "/accounts/account/cfd_tunnel":
			writeEnvelope(t, writer, http.StatusOK, []tunnelRecord{{
				ID: "11111111-1111-1111-1111-111111111111", Name: tunnelName("1234567890abcdef"), ConfigSrc: "cloudflare",
			}})
		case request.Method == http.MethodPut && strings.Contains(request.URL.Path, "/configurations"):
			var body struct {
				Config struct {
					Ingress []struct {
						Service string `json:"service"`
					} `json:"ingress"`
				} `json:"config"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
			} else if len(body.Config.Ingress) < 1 || body.Config.Ingress[0].Service != "http://127.19.53.87:8080" {
				t.Errorf("unexpected isolated origin: %#v", body.Config.Ingress)
			}
			writeEnvelope(t, writer, http.StatusOK, map[string]any{})
		case request.Method == http.MethodPut && strings.Contains(request.URL.Path, "/dns_records/"):
			writeEnvelope(t, writer, http.StatusOK, dnsRecord{ID: strings.Repeat("d", 32), Name: "demo.kidgrow.site"})
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/token"):
			writer.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(writer).Encode(map[string]any{"success": false, "errors": []map[string]any{{"code": 1000, "message": "temporary"}}})
		default:
			t.Errorf("unexpected request: %s %s", request.Method, request.URL.String())
			writeEnvelope(t, writer, http.StatusNotFound, nil)
		}
	}))
	defer server.Close()
	client, err := newClient(strings.Repeat("a", 32), server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnsureDeployment(context.Background(), testDeploymentSpec()); err == nil {
		t.Fatal("connector token failure was not returned")
	}
	mu.Lock()
	defer mu.Unlock()
	if deletes != 0 {
		t.Fatalf("deleted %d preexisting resources during rollback", deletes)
	}
}

func TestDeleteDeploymentFindsOwnedResourcesWhenStateIDsAreMissing(t *testing.T) {
	projectID := "1234567890abcdef"
	dnsID := strings.Repeat("d", 32)
	tunnelID := "11111111-1111-1111-1111-111111111111"
	deleted := make(map[string]bool)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && strings.Contains(request.URL.Path, "/dns_records"):
			writeEnvelope(t, writer, http.StatusOK, []dnsRecord{
				{ID: dnsID, Name: "demo.kidgrow.site", Comment: managedComment(projectID)},
				{ID: strings.Repeat("e", 32), Name: "demo.kidgrow.site", Comment: "managed elsewhere"},
			})
		case request.Method == http.MethodDelete && strings.Contains(request.URL.Path, "/dns_records/"):
			deleted[request.URL.Path] = true
			writeEnvelope(t, writer, http.StatusOK, map[string]any{})
		case request.Method == http.MethodGet && request.URL.Path == "/accounts/account/cfd_tunnel":
			writeEnvelope(t, writer, http.StatusOK, []tunnelRecord{
				{ID: tunnelID, Name: tunnelName(projectID), ConfigSrc: "cloudflare"},
				{ID: "22222222-2222-2222-2222-222222222222", Name: "unrelated", ConfigSrc: "cloudflare"},
			})
		case request.Method == http.MethodDelete && strings.Contains(request.URL.Path, "/cfd_tunnel/"):
			deleted[request.URL.Path] = true
			writeEnvelope(t, writer, http.StatusOK, map[string]any{})
		default:
			t.Errorf("unexpected request: %s %s", request.Method, request.URL.String())
			writeEnvelope(t, writer, http.StatusNotFound, nil)
		}
	}))
	defer server.Close()
	client, err := newClient(strings.Repeat("a", 32), server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteDeployment(context.Background(), testDeploymentSpec()); err != nil {
		t.Fatal(err)
	}
	if !deleted["/zones/zone/dns_records/"+dnsID] || !deleted["/accounts/account/cfd_tunnel/"+tunnelID] || len(deleted) != 2 {
		t.Fatalf("unexpected cleanup targets: %#v", deleted)
	}
}

func TestProjectOriginAddressIsStableAndSeparated(t *testing.T) {
	first := originAddress("1234567890abcdef")
	second := originAddress("1234577890abcdef")
	if first != "127.19.53.87:8080" {
		t.Fatalf("unexpected origin address: %s", first)
	}
	if first == second || !strings.HasPrefix(second, "127.") {
		t.Fatalf("project origins are not separated: %s %s", first, second)
	}
}

func testDeploymentSpec() DeploymentSpec {
	return DeploymentSpec{
		ProjectID: "1234567890abcdef", Hostname: "demo.kidgrow.site",
		Zone: Zone{Name: "kidgrow.site", ZoneID: "zone", AccountID: "account"},
	}
}

func writeEnvelope(t *testing.T, writer http.ResponseWriter, status int, result any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(map[string]any{"success": status >= 200 && status < 300, "result": result}); err != nil {
		t.Error(err)
	}
}
