package discovery

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
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
	Manufacturer     string
	Model            string
	SupportsProfileS bool
	SupportsProfileT bool
	SupportsProfileG bool
}

// WSDiscoveryClient handles multicast probing
type WSDiscoveryClient struct {
	sockets []*net.UDPConn
}

func NewWSDiscoveryClient() (*WSDiscoveryClient, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to get interfaces: %w", err)
	}

	var sockets []*net.UDPConn
	for _, iface := range ifaces {
		// Filter for active, multicast-capable, non-loopback interfaces
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagMulticast == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}

			// Bind to specific local IP to force interface
			localAddr, _ := net.ResolveUDPAddr("udp4", ipnet.IP.String()+":0")
			conn, err := net.ListenUDP("udp4", localAddr)
			if err != nil {
				continue
			}
			sockets = append(sockets, conn)
		}
	}

	if len(sockets) == 0 {
		return nil, errors.New("no suitable network interfaces found for discovery")
	}

	return &WSDiscoveryClient{sockets: sockets}, nil
}

func (c *WSDiscoveryClient) Close() {
	for _, s := range c.sockets {
		if s != nil {
			s.Close()
		}
	}
}

// Scan sends a probe and collects responses for duration
func (c *WSDiscoveryClient) Scan(ctx context.Context, duration time.Duration) ([]DiscoveredDevice, error) {
	probeUUID := uuid.New().String()
	probeMsg := buildProbeMessage(probeUUID)
	dstAddr, _ := net.ResolveUDPAddr("udp4", WSDiscoveryAddr)

	devicesMap := make(map[string]DiscoveredDevice)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, socket := range c.sockets {
		wg.Add(1)
		go func(conn *net.UDPConn) {
			defer wg.Done()
			localAddr := conn.LocalAddr().String()
			log.Printf("Discovery: Probing on %s", localAddr)

			// Send Probe
			if _, err := conn.WriteToUDP([]byte(probeMsg), dstAddr); err != nil {
				log.Printf("Discovery: Failed to write to %s: %v", localAddr, err)
				return
			}

			// Collection Loop
			conn.SetReadDeadline(time.Now().Add(duration))
			buf := make([]byte, MaxPacketSize)
			endTime := time.Now().Add(duration)

			for time.Now().Before(endTime) {
				remaining := time.Until(endTime)
				if remaining <= 0 {
					break
				}
				conn.SetReadDeadline(time.Now().Add(remaining))

				n, from, err := conn.ReadFromUDP(buf)
				if err != nil {
					// Timeout is normal end of scan
					return
				}

				if n > 0 {
					log.Printf("Discovery: Received %d bytes from %s on %s", n, from, localAddr)
					msg := buf[:n]
					dev, ok := parseProbeMatch(msg)
					if ok {
						mu.Lock()
						key := dev.EndpointRef
						if key == "" && len(dev.XAddrs) > 0 {
							key = dev.XAddrs[0]
						}
						if key != "" {
							devicesMap[key] = dev
						}
						mu.Unlock()
					}
				}
			}
		}(socket)
	}

	wg.Wait()

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
		log.Printf("Discovery: XML Unmarshal Error: %v", err)
		return DiscoveredDevice{}, false
	}

	if len(env.Body.ProbeMatches.ProbeMatch) == 0 {
		log.Printf("Discovery: No ProbeMatch in Envelope")
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

	// Fallback IP from EndpointRef if needed? No, EndpointRef is usually UUID.

	log.Printf("Discovery: Parsed Device - Endpoint: %s, XAddrs: %v, Scopes: %d", match.EndpointReference.Address, xaddrs, len(scopes))

	// Profile Hints & Metadata
	s, t, g := detectProfileHints(scopes)
	manufacturer, model := parseScopes(scopes)

	return DiscoveredDevice{
		EndpointRef:      match.EndpointReference.Address,
		XAddrs:           xaddrs,
		Scopes:           scopes,
		Types:            types,
		IPAddress:        ip,
		Manufacturer:     manufacturer,
		Model:            model,
		SupportsProfileS: s,
		SupportsProfileT: t,
		SupportsProfileG: g,
	}, true
}

func parseScopes(scopes []string) (mfr, model string) {
	isNVT := false
	for _, s := range scopes {
		lower := strings.ToLower(s)
		
		if strings.Contains(lower, "/type/network_video_transmitter") {
			isNVT = true
		}

		// Standard ONVIF Name/Hardware
		if strings.Contains(lower, "/name/") {
			val := extractScopeValue(s, "/name/")
			if val != "" { mfr = val }
		} else if strings.Contains(lower, "/hardware/") {
			val := extractScopeValue(s, "/hardware/")
			if val != "" { model = val }
		} else if strings.Contains(lower, "/model/") {
			val := extractScopeValue(s, "/model/")
			if val != "" { model = val }
		} else if strings.Contains(lower, "/manufacturer/") {
			val := extractScopeValue(s, "/manufacturer/")
			if val != "" { mfr = val }
		}
	}

	if mfr == "" && isNVT {
		mfr = "ONVIF"
	}
	return
}

func extractScopeValue(scope, key string) string {
	idx := strings.Index(strings.ToLower(scope), key)
	if idx == -1 { return "" }
	val := scope[idx+len(key):]
	val = strings.ReplaceAll(val, "_", " ")
	return strings.TrimSpace(val)
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
