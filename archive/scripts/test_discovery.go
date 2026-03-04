//go:build ignore

package main

import (
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	WSDiscoveryAddr = "239.255.255.250:3702"
	MaxPacketSize   = 8192
)

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

func main() {
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Fatalf("Failed to get interfaces: %v", err)
	}

	dstAddr, _ := net.ResolveUDPAddr("udp4", WSDiscoveryAddr)

	for _, iface := range ifaces {
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

			fmt.Printf("Testing on interface %s (IP: %s)...\n", iface.Name, ipnet.IP.String())

			// Bind to specific local IP to force interface
			localAddr, _ := net.ResolveUDPAddr("udp4", ipnet.IP.String()+":0")
			conn, err := net.ListenUDP("udp4", localAddr)
			if err != nil {
				fmt.Printf("  Failed to bind: %v\n", err)
				continue
			}

			probeUUID := uuid.New().String()
			probeMsg := buildProbeMessage(probeUUID)

			if _, err := conn.WriteToUDP([]byte(probeMsg), dstAddr); err != nil {
				fmt.Printf("  Failed to send: %v\n", err)
				conn.Close()
				continue
			}

			duration := 3 * time.Second
			conn.SetReadDeadline(time.Now().Add(duration))
			buf := make([]byte, MaxPacketSize)

			for {
				n, from, err := conn.ReadFromUDP(buf)
				if err != nil {
					if !strings.Contains(err.Error(), "timeout") {
						fmt.Printf("  Read error: %v\n", err)
					}
					break
				}
				fmt.Printf("  [FOUND] Response from %s (%d bytes)\n", from.String(), n)
				// fmt.Println(string(buf[:n]))
			}
			conn.Close()
		}
	}
	fmt.Println("Scan finished.")
}
