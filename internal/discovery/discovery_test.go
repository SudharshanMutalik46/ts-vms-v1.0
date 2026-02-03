package discovery

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/audit"
	"github.com/technosupport/ts-vms/internal/data"
)

func TestParseProbeMatch(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope" xmlns:wsa="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery">
   <soap:Header>
      <wsa:MessageID>uuid:1234</wsa:MessageID>
   </soap:Header>
   <soap:Body>
      <d:ProbeMatches>
         <d:ProbeMatch>
            <wsa:EndpointReference>
               <wsa:Address>urn:uuid:0000-0000-0000-0000</wsa:Address>
            </wsa:EndpointReference>
            <d:Types>dn:NetworkVideoTransmitter</d:Types>
            <d:Scopes>onvif://www.onvif.org/Profile/S onvif://www.onvif.org/hardware/ModelA</d:Scopes>
            <d:XAddrs>http://192.168.1.100/onvif/device_service</d:XAddrs>
            <d:MetadataVersion>1</d:MetadataVersion>
         </d:ProbeMatch>
      </d:ProbeMatches>
   </soap:Body>
</soap:Envelope>`

	dev, ok := parseProbeMatch([]byte(xml))
	if !ok {
		t.Fatal("Failed to parse valid ProbeMatch")
	}
	if dev.IPAddress != "192.168.1.100" {
		t.Errorf("Expected IP 192.168.1.100, got %s", dev.IPAddress)
	}
	if !dev.SupportsProfileS {
		t.Error("Failed to detect Profile S hint")
	}
	if dev.EndpointRef != "urn:uuid:0000-0000-0000-0000" {
		t.Error("Wrong EndpointRef")
	}
}

func TestIPv4Extraction(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"http://192.168.1.50/onvif", "192.168.1.50"},
		{"http://192.168.1.50:8080/onvif", "192.168.1.50"},
		{"https://10.0.0.1/device", "10.0.0.1"},
		{"invalid", ""},
	}
	for _, c := range cases {
		got := extractIPv4([]string{c.input})
		if got != c.want {
			t.Errorf("extractIPv4(%s) = %s; want %s", c.input, got, c.want)
		}
	}
}

// Mocks
type MockRepo struct {
	Runs map[string]*data.DiscoveryRun
	Devs map[string]*data.DiscoveredDevice
}

func (m *MockRepo) CreateRun(ctx context.Context, r *data.DiscoveryRun) error {
	r.ID = uuid.New()
	m.Runs[r.ID.String()] = r
	return nil
}
func (m *MockRepo) UpdateRunStatus(ctx context.Context, id uuid.UUID, s string, f bool, d, e int) error {
	if r, ok := m.Runs[id.String()]; ok {
		r.Status = s
	}
	return nil
}
func (m *MockRepo) UpsertDevice(ctx context.Context, d *data.DiscoveredDevice) error {
	// Mock upsert (always create for test simplicity unless ID set)
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	m.Devs[d.ID.String()] = d
	return nil
}
func (m *MockRepo) UpdateDeviceProbe(ctx context.Context, d *data.DiscoveredDevice) error {
	m.Devs[d.ID.String()] = d
	return nil
}
func (m *MockRepo) GetDevice(ctx context.Context, id uuid.UUID) (*data.DiscoveredDevice, error) {
	if d, ok := m.Devs[id.String()]; ok {
		return d, nil
	}
	return nil, data.ErrDeviceNotFound
}
func (m *MockRepo) GetRun(ctx context.Context, id uuid.UUID) (*data.DiscoveryRun, error) {
	return nil, nil
}
func (m *MockRepo) ListDevices(ctx context.Context, id uuid.UUID, l, o int) ([]*data.DiscoveredDevice, error) {
	return nil, nil
}
func (m *MockRepo) StoreBootstrapCred(ctx context.Context, c *data.OnvifCredential) error { return nil }
func (m *MockRepo) GetBootstrapCred(ctx context.Context, id uuid.UUID) (*data.OnvifCredential, error) {
	return nil, nil
}

type MockAuditor struct{ Events []audit.AuditEvent }

func (m *MockAuditor) WriteEvent(ctx context.Context, evt audit.AuditEvent) error {
	m.Events = append(m.Events, evt)
	return nil
}

func TestStartDiscovery(t *testing.T) {
	// Integration-lite test for service orchestration
	repo := &MockRepo{Runs: make(map[string]*data.DiscoveryRun), Devs: make(map[string]*data.DiscoveredDevice)}
	aud := &MockAuditor{}
	svc := NewService(repo, nil, aud)

	// Test Async Start
	uid := uuid.New()
	id, err := svc.StartDiscovery(context.Background(), uid, nil)
	if err != nil {
		t.Fatalf("StartDiscovery failed: %v", err)
	}

	// Check Run Created
	if _, ok := repo.Runs[id.String()]; !ok {
		t.Error("Run not created in repo")
	}

	// Check Audit Event
	if len(aud.Events) == 0 {
		t.Error("Audit event not emitted")
	} else {
		if aud.Events[0].Action != "onvif.discovery.run" {
			t.Errorf("Wrong audit action: %s", aud.Events[0].Action)
		}
	}
}
