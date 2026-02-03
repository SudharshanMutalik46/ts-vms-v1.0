package discovery

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	WSDiscoveryAddr = "239.255.255.250:3702"
	MaxPacketSize   = 4096
)

// Simplified Envelope for ProbeMatches
type Envelope struct {
	XMLName xml.Name `xml:"http://www.w3.org/2003/05/soap-envelope Envelope"`
	Body    Body
}

type Body struct {
	ProbeMatches ProbeMatches `xml:"http://schemas.xmlsoap.org/ws/2005/04/discovery ProbeMatches"`
}

type ProbeMatches struct {
	ProbeMatch []ProbeMatch `xml:"ProbeMatch"`
}

type ProbeMatch struct {
	EndpointReference EndpointReference
	Types             string `xml:"Types"`
	Scopes            string `xml:"Scopes"`
	XAddrs            string `xml:"XAddrs"`
	MetadataVersion   int    `xml:"MetadataVersion"`
}

type EndpointReference struct {
	Address string `xml:"Address"`
}

type DiscoveredDevice struct {
	ID               string
	IPAddress        string // Best guess
	XAddrs           []string
	Scopes           []string
	Types            []string
	EndpointRef      string
	SupportsProfileS bool
	SupportsProfileT bool
	SupportsProfileG bool
}

// WSDiscoveryClient handles multicast probing
type WSDiscoveryClient struct {
	socket *net.UDPConn
}

func NewWSDiscoveryClient() (*WSDiscoveryClient, error) {
	// Binding to specific interface is complex on Windows for Multicast without raw sockets or specific config.
	// Standard approach: Bind to 0.0.0.0 and valid port (0 for ephemeral)
	// For sending multicast, standard UDP conn is fine.
	// For receiving, we need to join group.
	// Windows idiosyncrasy: Sometimes improved by binding to specific IP if multiple NICs.
	// We'll start with standard 0.0.0.0 listen.
	// If needed, we'd iterate interfaces.

	addr, _ := net.ResolveUDPAddr("udp4", ":0") // Ephemeral port
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to bind udp: %w", err)
	}

	return &WSDiscoveryClient{socket: conn}, nil
}

func (c *WSDiscoveryClient) Close() {
	if c.socket != nil {
		c.socket.Close()
	}
}

// Scan sends a probe and collects responses for duration
func (c *WSDiscoveryClient) Scan(ctx context.Context, duration time.Duration) ([]DiscoveredDevice, error) {
	probeUUID := uuid.New().String()
	probeMsg := buildProbeMessage(probeUUID)

	dstAddr, _ := net.ResolveUDPAddr("udp4", WSDiscoveryAddr)

	// Send Probe
	if _, err := c.socket.WriteToUDP([]byte(probeMsg), dstAddr); err != nil {
		return nil, fmt.Errorf("failed to send probe: %w", err)
	}

	// Collection Loop
	devicesMap := make(map[string]DiscoveredDevice)

	// Deadline
	c.socket.SetReadDeadline(time.Now().Add(duration))

	buf := make([]byte, MaxPacketSize)

	endTime := time.Now().Add(duration)
	for time.Now().Before(endTime) {
		// Calculate remaining read time
		remaining := time.Until(endTime)
		if remaining <= 0 {
			break
		}
		c.socket.SetReadDeadline(time.Now().Add(remaining))

		n, _, err := c.socket.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "timeout") {
				break
			}
			// Other errors (e.g., closed)
			// On Windows, ICMP unreachable might cause error, just continue?
			// Best to log and continue usually, but we stop on fatal.
			// Timeout is expected exit.
			break
		}

		if n > 0 {
			msg := buf[:n]
			dev, ok := parseProbeMatch(msg)
			if ok {
				// De-dupe by EndpointRef or IP+XAddrs
				key := dev.EndpointRef
				if key == "" && len(dev.XAddrs) > 0 {
					key = dev.XAddrs[0]
				}
				if key != "" {
					devicesMap[key] = dev
				}
			}
		}
	}

	results := make([]DiscoveredDevice, 0, len(devicesMap))
	for _, dev := range devicesMap {
		results = append(results, dev)
	}
	return results, nil
}

func buildProbeMessage(msgID string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<e:Envelope xmlns:e="http://www.w3.org/2003/05/soap-envelope"
            xmlns:w="http://schemas.xmlsoap.org/ws/2005/04/discovery"
            xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery"
            xmlns:dn="http://www.onvif.org/ver10/network/wsdl">
    <e:Header>
        <w:MessageID>uuid:` + msgID + `</w:MessageID>
        <w:To e:mustUnderstand="true">urn:schemas-xmlsoap-org:ws:2005:04:discovery</w:To>
        <w:Action a:mustUnderstand="true">http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</w:Action>
    </e:Header>
    <e:Body>
        <d:Probe>
            <d:Types>dn:NetworkVideoTransmitter</d:Types>
        </d:Probe>
    </e:Body>
</e:Envelope>`
}

func parseProbeMatch(data []byte) (DiscoveredDevice, bool) {
	var env Envelope
	// Handle XML namespace prefix issues by being lax or just standard Go unmarshal
	// Usually ONVIF returns fairly standard SOAP.
	if err := xml.Unmarshal(data, &env); err != nil {
		return DiscoveredDevice{}, false
	}

	if len(env.Body.ProbeMatches.ProbeMatch) == 0 {
		return DiscoveredDevice{}, false
	}

	match := env.Body.ProbeMatches.ProbeMatch[0]

	// Parse XAddrs
	xaddrs := strings.Fields(match.XAddrs)
	scopes := strings.Fields(match.Scopes)
	types := strings.Fields(match.Types)

	// Extract IP
	// Simple heuristic: Take first IPv4 from XAddrs
	ip := extractIPv4(xaddrs)

	// Profile Hints
	s, t, g := detectProfileHints(scopes)

	return DiscoveredDevice{
		EndpointRef:      match.EndpointReference.Address,
		XAddrs:           xaddrs,
		Scopes:           scopes,
		Types:            types,
		IPAddress:        ip,
		SupportsProfileS: s,
		SupportsProfileT: t,
		SupportsProfileG: g,
	}, true
}

func extractIPv4(xaddrs []string) string {
	for _, x := range xaddrs {
		// Look for http://IP:PORT/ or similar
		// We can use url.Parse, but simple split logic often enough for discovery
		// Strip http://
		s := strings.TrimPrefix(x, "http://")
		s = strings.TrimPrefix(s, "https://")
		// Get host part
		host, _, err := net.SplitHostPort(s)
		if err != nil {
			// Maybe no port
			host = s
			if idx := strings.Index(s, "/"); idx != -1 {
				host = s[:idx]
			}
		}

		// Check if valid IP
		parsed := net.ParseIP(host)
		if parsed != nil && parsed.To4() != nil && !parsed.IsLoopback() {
			return host
		}
	}
	return ""
}

func detectProfileHints(scopes []string) (s, t, g bool) {
	for _, sc := range scopes {
		// Standard Scope: onvif://www.onvif.org/Profile/S
		lower := strings.ToLower(sc)
		if strings.Contains(lower, "profile/s") {
			s = true
		}
		if strings.Contains(lower, "profile/t") {
			t = true
		}
		if strings.Contains(lower, "profile/g") {
			g = true
		}
	}
	return
}
