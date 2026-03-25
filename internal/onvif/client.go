package onvif

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OnvifClient handles SOAP requests
type OnvifClient struct {
	BaseURL  string
	Username string
	Password string
	HTTP     *http.Client
}

func NewOnvifClient(xaddr, username, password string) (*OnvifClient, error) {
	// Ensure valid URL
	u, err := url.Parse(xaddr)
	if err != nil {
		return nil, err
	}
	return &OnvifClient{
		BaseURL:  u.String(),
		Username: username,
		Password: password,
		HTTP:     &http.Client{Timeout: 2 * time.Second}, // Per-call timeout limit (Requirement)
	}, nil
}

// SOAP Envelope generic
type SOAPEnvelope struct {
	XMLName xml.Name `xml:"http://www.w3.org/2003/05/soap-envelope Envelope"`
	Header  SOAPHeader
	Body    SOAPBody
}

type SOAPHeader struct {
	Security *Security `xml:"http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd Security,omitempty"`
}

type Security struct {
	UsernameToken UsernameToken
}

type UsernameToken struct {
	Username string
	Password Password
	Nonce    string
	Created  string
}

type Password struct {
	Type  string `xml:"Type,attr"`
	Value string `xml:",chardata"`
}

type SOAPBody struct {
	Content []byte `xml:",innerxml"`
}

// GetDeviceInformation
type GetDeviceInformationResponse struct {
	Manufacturer    string `xml:"Manufacturer"`
	Model           string `xml:"Model"`
	FirmwareVersion string `xml:"FirmwareVersion"`
	SerialNumber    string `xml:"SerialNumber"`
	HardwareId      string `xml:"HardwareId"`
}

func (c *OnvifClient) GetDeviceInformation(ctx context.Context) (*GetDeviceInformationResponse, error) {
	reqBody := `<tds:GetDeviceInformation xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>`
	resp, err := c.Do(ctx, reqBody)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Body struct {
			GetDeviceInformationResponse GetDeviceInformationResponse `xml:"GetDeviceInformationResponse"`
		}
	}
	if err := xml.Unmarshal(resp, &parsed); err != nil {
		return nil, err
	}
	return &parsed.Body.GetDeviceInformationResponse, nil
}

// GetCapabilities (Lightweight)
// Returns Media Service Address if available, used for Media calls
func (c *OnvifClient) GetCapabilities(ctx context.Context) (map[string]bool, string, string, string, error) {
	reqBody := `<tds:GetCapabilities xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
		<tds:Category>All</tds:Category>
	</tds:GetCapabilities>`

	resp, err := c.Do(ctx, reqBody)
	if err != nil {
		return nil, "", "", "", err
	}

	// Simple structure to extract Media XAddr
	var caps struct {
		Body struct {
			GetCapabilitiesResponse struct {
				Capabilities struct {
					Media struct {
						XAddr string `xml:"XAddr"`
					} `xml:"Media"`
					Events struct {
						XAddr string `xml:"XAddr"`
					} `xml:"Events"`
					Extension struct {
						Media2 struct {
							XAddr string `xml:"XAddr"`
						} `xml:"Media2"`
					} `xml:"Extension"`
				} `xml:"Capabilities"`
			} `xml:"GetCapabilitiesResponse"`
		}
	}

	if err := xml.Unmarshal(resp, &caps); err != nil {
		return nil, "", "", "", err
	}

	features := make(map[string]bool)
	mediaXAddr := caps.Body.GetCapabilitiesResponse.Capabilities.Media.XAddr
	media2XAddr := caps.Body.GetCapabilitiesResponse.Capabilities.Extension.Media2.XAddr
	eventsXAddr := caps.Body.GetCapabilitiesResponse.Capabilities.Events.XAddr

	if mediaXAddr != "" {
		features["Media"] = true
	}
	if media2XAddr != "" {
		features["Media2"] = true
	}
	if eventsXAddr != "" {
		features["Events"] = true
	}

	// If Media2 not found in Capabilites, try GetServices
	if media2XAddr == "" {
		services, err := c.GetServices(ctx)
		if err == nil {
			if m2, ok := services["http://www.onvif.org/ver20/media/wsdl"]; ok {
				media2XAddr = m2
				features["Media2"] = true
			}
		}
	}

	return features, mediaXAddr, eventsXAddr, media2XAddr, nil
}

type Resolution struct {
	Width  int `xml:"Width"`
	Height int `xml:"Height"`
}

type RateControl struct {
	FrameRateLimit float64 `xml:"FrameRateLimit"`
	BitrateLimit   int     `xml:"BitrateLimit"`
}

type VideoEncoderConfiguration struct {
	Token       string      `xml:"token,attr"`
	Name        string      `xml:"Name"`
	Encoding    string      `xml:"Encoding"`
	Resolution  Resolution  `xml:"Resolution"`
	RateControl RateControl `xml:"RateControl"`
}

// MediaProfile
type MediaProfile struct {
	Token string `xml:"token,attr"`
	Name  string `xml:"Name"`

	// Media1 Compatibility
	VideoEncoderConfiguration *VideoEncoderConfiguration `xml:"VideoEncoderConfiguration"`
	AudioSourceConfiguration  *struct{}                  `xml:"AudioSourceConfiguration"`
	AudioEncoderConfiguration *struct{}                  `xml:"AudioEncoderConfiguration"`
	PTZConfiguration          *struct{}                  `xml:"PTZConfiguration"`

	// Media2 Compatibility
	Configurations *struct {
		VideoEncoder *VideoEncoderConfiguration `xml:"VideoEncoder"`
		AudioSource  *struct{}                  `xml:"AudioSource"`
		AudioEncoder *struct{}                  `xml:"AudioEncoder"`
		PTZ          *struct{}                  `xml:"PTZ"`
	} `xml:"Configurations"`
}

func (c *OnvifClient) GetProfiles(ctx context.Context, mediaURI string) ([]MediaProfile, error) {
	// Create temporary client for Media URI if different from Device URI
	mediaClient := c
	if mediaURI != "" && mediaURI != c.BaseURL {
		mc, _ := NewOnvifClient(mediaURI, c.Username, c.Password)
		mediaClient = mc
	}

	reqBody := `<trt:GetProfiles xmlns:trt="http://www.onvif.org/ver10/media/wsdl"/>`
	resp, err := mediaClient.Do(ctx, reqBody)
	if err != nil {
		return nil, err
	}
	// DEBUG: log raw XML for field mapping inspection
	log.Printf("[DEBUG] GetProfiles Raw Response for %s: %s\n", c.BaseURL, string(resp))

	var parsed struct {
		Body struct {
			GetProfilesResponse struct {
				Profiles []MediaProfile `xml:"Profiles"`
			} `xml:"GetProfilesResponse"`
		}
	}
	if err := xml.Unmarshal(resp, &parsed); err != nil {
		return nil, err
	}

	profiles := parsed.Body.GetProfilesResponse.Profiles
	for i := range profiles {
		p := &profiles[i]
		// Normalize Media2 into Media1 structure for internal use
		if p.VideoEncoderConfiguration == nil && p.Configurations != nil && p.Configurations.VideoEncoder != nil {
			p.VideoEncoderConfiguration = p.Configurations.VideoEncoder
		}
		if p.AudioSourceConfiguration == nil && p.Configurations != nil && p.Configurations.AudioSource != nil {
			p.AudioSourceConfiguration = p.Configurations.AudioSource
		}
		if p.AudioEncoderConfiguration == nil && p.Configurations != nil && p.Configurations.AudioEncoder != nil {
			p.AudioEncoderConfiguration = p.Configurations.AudioEncoder
		}
		if p.PTZConfiguration == nil && p.Configurations != nil && p.Configurations.PTZ != nil {
			p.PTZConfiguration = p.Configurations.PTZ
		}
	}
	return profiles, nil
}

// GetStreamUri
func (c *OnvifClient) GetServices(ctx context.Context) (map[string]string, error) {
	reqBody := `<tds:GetServices xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
		<tds:IncludeCapability>false</tds:IncludeCapability>
	</tds:GetServices>`

	resp, err := c.Do(ctx, reqBody)
	if err != nil {
		return nil, err
	}

	var res struct {
		Body struct {
			GetServicesResponse struct {
				Service []struct {
					Namespace string `xml:"Namespace"`
					XAddr     string `xml:"XAddr"`
				} `xml:"Service"`
			} `xml:"GetServicesResponse"`
		}
	}

	if err := xml.Unmarshal(resp, &res); err != nil {
		return nil, err
	}

	services := make(map[string]string)
	for _, s := range res.Body.GetServicesResponse.Service {
		services[s.Namespace] = s.XAddr
	}
	return services, nil
}

func (c *OnvifClient) GetStreamUri(ctx context.Context, mediaURI, token string, useMedia2 bool) (string, error) {
	mediaClient := c
	if mediaURI != "" && mediaURI != c.BaseURL {
		mc, _ := NewOnvifClient(mediaURI, c.Username, c.Password)
		mediaClient = mc
	}

	var reqBody string
	if useMedia2 {
		// Media2 (ver20) namespace
		reqBody = fmt.Sprintf(`<tr2:GetStreamUri xmlns:tr2="http://www.onvif.org/ver20/media/wsdl">
			<tr2:Protocol>RTSP</tr2:Protocol>
			<tr2:ProfileToken>%s</tr2:ProfileToken>
		</tr2:GetStreamUri>`, token)
	} else {
		// Media1 (ver10) namespace
		reqBody = fmt.Sprintf(`<trt:GetStreamUri xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
			<trt:StreamSetup>
				<trt:Stream xmlns:tt="http://www.onvif.org/ver10/schema">tt:RTP-Unicast</trt:Stream>
				<trt:Transport xmlns:tt="http://www.onvif.org/ver10/schema">
					<tt:Protocol>tt:RTSP</tt:Protocol>
				</trt:Transport>
			</trt:StreamSetup>
			<trt:ProfileToken>%s</trt:ProfileToken>
		</trt:GetStreamUri>`, token)
	}

	resp, err := mediaClient.Do(ctx, reqBody)
	if err != nil {
		return "", err
	}

	var parsed struct {
		Body struct {
			GetStreamUriResponse struct {
				Uri      string `xml:"Uri"`      // Media2
				MediaUri struct {
					Uri string `xml:"Uri"`  // Media1
				} `xml:"MediaUri"`
			} `xml:"GetStreamUriResponse"`
		}
	}

	if err := xml.Unmarshal(resp, &parsed); err != nil {
		return c.detectWorkingRtspUri(token), nil
	}

	uri := parsed.Body.GetStreamUriResponse.Uri
	if uri == "" {
		uri = parsed.Body.GetStreamUriResponse.MediaUri.Uri
	}

	if uri == "" {
		return c.detectWorkingRtspUri(token), nil
	}

	// RTSP Verification
	parsedUri, err := url.Parse(uri)
	if err == nil {
		baseU, _ := url.Parse(c.BaseURL)
		code, _ := c.checkRtspPathCode(baseU.Hostname(), "554", parsedUri.Path)

		// FIX: Modern cameras return 401 for DESCRIBE. This is FINE and means the path exists.
		// Only fallback if the connection failed (0) or it's a known bad Hikvision 101 path.
		isBadHikvision := (strings.Contains(uri, "Channels/101") || strings.Contains(uri, "101"))
		if code == 0 || (code == 200 && isBadHikvision) {
			fmt.Printf("[ONVIF] Camera returned URI %s (Code %d). Treating as suspicious/failed. Fallback to Fuzzer.\n", uri, code)
			return c.detectWorkingRtspUri(token), nil
		}
	}
	return uri, nil
}

func (c *OnvifClient) GetNetworkInterfaces(ctx context.Context) (string, error) {
	reqBody := `<tds:GetNetworkInterfaces xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>`
	resp, err := c.Do(ctx, reqBody)
	if err != nil {
		return "", err
	}

	var parsed struct {
		Body struct {
			GetNetworkInterfacesResponse struct {
				NetworkInterfaces []struct {
					Info struct {
						HwAddress string
					}
				}
			} `xml:"GetNetworkInterfacesResponse"`
		}
	}
	if err := xml.Unmarshal(resp, &parsed); err != nil {
		return "", err
	}
	for _, ni := range parsed.Body.GetNetworkInterfacesResponse.NetworkInterfaces {
		if ni.Info.HwAddress != "" {
			return strings.ToUpper(ni.Info.HwAddress), nil
		}
	}
	return "", nil
}

func (c *OnvifClient) GetSystemDateAndTime(ctx context.Context) (int, error) {
	reqBody := `<tds:GetSystemDateAndTime xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>`
	resp, err := c.Do(ctx, reqBody)
	if err != nil {
		return 0, err
	}

	var parsed struct {
		Body struct {
			GetSystemDateAndTimeResponse struct {
				SystemDateAndTime struct {
					UTCDateTime struct {
						Time struct {
							Hour   int
							Minute int
							Second int
						}
						Date struct {
							Year  int
							Month int
							Day   int
						}
					}
				}
			} `xml:"GetSystemDateAndTimeResponse"`
		}
	}
	if err := xml.Unmarshal(resp, &parsed); err != nil {
		return 0, err
	}
	dt := parsed.Body.GetSystemDateAndTimeResponse.SystemDateAndTime.UTCDateTime
	deviceTime := time.Date(dt.Date.Year, time.Month(dt.Date.Month), dt.Date.Day, dt.Time.Hour, dt.Time.Minute, dt.Time.Second, 0, time.UTC)
	offset := int(deviceTime.Sub(time.Now().UTC()).Seconds())
	return offset, nil
}

func (c *OnvifClient) GetAudioSources(ctx context.Context, mediaURI string) (int, error) {
	mediaClient := c
	if mediaURI != "" && mediaURI != c.BaseURL {
		mc, _ := NewOnvifClient(mediaURI, c.Username, c.Password)
		mediaClient = mc
	}

	reqBody := `<trt:GetAudioSources xmlns:trt="http://www.onvif.org/ver10/media/wsdl"/>`
	resp, err := mediaClient.Do(ctx, reqBody)
	if err != nil {
		return 0, err
	}

	var parsed struct {
		Body struct {
			GetAudioSourcesResponse struct {
				AudioSources []struct{} `xml:"AudioSources"`
			} `xml:"GetAudioSourcesResponse"`
		}
	}
	if err := xml.Unmarshal(resp, &parsed); err != nil {
		return 0, err
	}
	return len(parsed.Body.GetAudioSourcesResponse.AudioSources), nil
}

func (c *OnvifClient) GetSnapshotURI(ctx context.Context, mediaURI, token string) (string, error) {
	mediaClient := c
	if mediaURI != "" && mediaURI != c.BaseURL {
		mc, _ := NewOnvifClient(mediaURI, c.Username, c.Password)
		mediaClient = mc
	}

	reqBody := fmt.Sprintf(`<trt:GetSnapshotUri xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
		<trt:ProfileToken>%s</trt:ProfileToken>
	</trt:GetSnapshotUri>`, token)

	resp, err := mediaClient.Do(ctx, reqBody)
	if err != nil {
		return "", err
	}

	var parsed struct {
		Body struct {
			GetSnapshotUriResponse struct {
				MediaUri struct {
					Uri string `xml:"Uri"`
				} `xml:"MediaUri"`
			} `xml:"GetSnapshotUriResponse"`
		}
	}
	if err := xml.Unmarshal(resp, &parsed); err != nil {
		return "", err
	}
	return parsed.Body.GetSnapshotUriResponse.MediaUri.Uri, nil
}

func (c *OnvifClient) GetEventProperties(ctx context.Context, eventsURI string) ([]string, error) {
	eventsClient := c
	if eventsURI != "" && eventsURI != c.BaseURL {
		ec, _ := NewOnvifClient(eventsURI, c.Username, c.Password)
		eventsClient = ec
	}

	reqBody := `<tev:GetEventProperties xmlns:tev="http://www.onvif.org/ver10/events/wsdl"/>`
	resp, err := eventsClient.Do(ctx, reqBody)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Body struct {
			GetEventPropertiesResponse struct {
				TopicSet struct {
					Inner []byte `xml:",innerxml"`
				} `xml:"TopicSet"`
			} `xml:"GetEventPropertiesResponse"`
		}
	}
	if err := xml.Unmarshal(resp, &parsed); err != nil {
		return nil, err
	}

	// Heuristic: Extract topic names from XML tags
	// Proper parsing of TopicSet is complex, but this regex-like approach works for discovery hints.
	topics := []string{}
	// Simple cleanup: find substrings between <tt: and >
	raw := string(parsed.Body.GetEventPropertiesResponse.TopicSet.Inner)
	// We'll just return a few common ones if they exist, or the raw keys
	if strings.Contains(raw, "MotionAlarm") {
		topics = append(topics, "Motion")
	}
	if strings.Contains(raw, "DigitalInput") {
		topics = append(topics, "DigitalInput")
	}
	if strings.Contains(raw, "Tampering") {
		topics = append(topics, "Tampering")
	}

	return topics, nil
}

// Do executes the SOAP request with Auth
func (c *OnvifClient) Do(ctx context.Context, bodyInner string) ([]byte, error) {
	envelope := `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
	<s:Header>%s</s:Header>
	<s:Body>%s</s:Body>
</s:Envelope>`

	header := c.generateCnonceHeader()
	payload := fmt.Sprintf(envelope, header, bodyInner)

	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL, bytes.NewBufferString(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8; action=\"\"")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("onvif error %d: %s", resp.StatusCode, string(errBytes))
	}

	return io.ReadAll(resp.Body)
}

// Capabilities summary
type CameraCapabilities struct {
	HasAudio bool
	PTZ      bool
}

func DetermineCapabilities(profiles []MediaProfile) CameraCapabilities {
	caps := CameraCapabilities{
		HasAudio: false,
		PTZ:      false,
	}

	for _, profile := range profiles {
		if profile.AudioSourceConfiguration != nil || profile.AudioEncoderConfiguration != nil {
			caps.HasAudio = true
		}
		if profile.PTZConfiguration != nil {
			caps.PTZ = true
		}
	}

	return caps
}

func (c *OnvifClient) generateCnonceHeader() string {
	if c.Username == "" {
		return ""
	}

	nonceRaw := make([]byte, 16)
	if _, err := rand.Read(nonceRaw); err != nil {
		copy(nonceRaw, []byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	}

	nonce := base64.StdEncoding.EncodeToString(nonceRaw)
	created := time.Now().UTC().Format(time.RFC3339Nano) 

	digest := computeSoapDigest(nonceRaw, created, c.Password)

	return fmt.Sprintf(`<Security xmlns="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd">
		<UsernameToken>
			<Username>%s</Username>
			<Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">%s</Password>
			<Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">%s</Nonce>
			<Created xmlns="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd">%s</Created>
		</UsernameToken>
	</Security>`, c.Username, digest, nonce, created)
}

func computeSoapDigest(nonce []byte, created, password string) string {
	h := sha1.New()
	h.Write(nonce)
	h.Write([]byte(created))
	h.Write([]byte(password))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func (c *OnvifClient) detectWorkingRtspUri(token string) string {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	port := "554" 

	fmt.Printf("[RTSP-Fuzz] Starting active discovery for %s...\n", host)

	candidates := []string{
		"/stream", "/live", "/video",
		"/live/main", "/live/sub", "/live/0",
		"/onvif1", "/onvif2", "/profile1", "/profile2",
		"/stream1", "/stream2", "/unicast", "/multicast",
		"/ch0_0.h264", "/live/1", "/live/2",
		"/cam1/h264", "/cam1/mjpeg",
		"/defaultPrimary?streamType=u",
		"/h265", "/mjpeg",
		"/cam/realmonitor?channel=1&subtype=0",
		"/cam/realmonitor?channel=1&subtype=1",
		"/live/0/MAIN", "/live/0/SUB", "/live/0/0", "/live/0/1",
		"/udp/av0_0", "/udp/av0_1", "/rtsp_live0", "/rtsp_live1",
		"/axis-media/media.amp", "/media/video1", "/media/video2",
		"/video1", "/1", "/2", "/11", "/12", "/0",
		"/ch1/main/av_stream", "/ch1/sub/av_stream",
		"/main", "/sub",
		"/mps/video/1", "/av0_0", "/av0_1", "/live/ch0", "/live/ch1",
		"/live/primary", "/live/secondary",
		"/h264/ch1/main/av_stream", "/h264", "/mpeg4", "/mpeg4cif",
		"/img/video.sav", "/live.sdp", "/play1.sdp",
		"/Streaming/Channels/101", "/Streaming/Channels/102",
	}

	var possibleCandidates []string
	for _, path := range candidates {
		code, resp := c.checkRtspPathCode(host, port, path)
		if code == 200 {
			fmt.Printf("[RTSP-Fuzz] SUCCESS (200 OK): %s\n RESPONSE: %s\n", path, resp)
			return fmt.Sprintf("rtsp://%s:%s%s", host, port, path)
		}
		if code == 401 {
			possibleCandidates = append(possibleCandidates, path)
			fmt.Printf("[RTSP-Fuzz] Potential (401 Auth): %s\n", path)
		}
	}

	if len(possibleCandidates) > 0 {
		best := possibleCandidates[0]
		fmt.Printf("[RTSP-Fuzz] No 200 OK found. Returning first 401 candidate: %s\n", best)
		return fmt.Sprintf("rtsp://%s:%s%s", host, port, best)
	}

	fmt.Println("[RTSP-Fuzz] All checks failed. Returning default.")
	return fmt.Sprintf("rtsp://%s:554/Streaming/Channels/101", host)
}

func (c *OnvifClient) checkRtspPathCode(host, port, path string) (int, string) {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%s", host, port), 2000*time.Millisecond)
	if err != nil {
		return 0, ""
	}
	defer conn.Close()

	msg := fmt.Sprintf("DESCRIBE rtsp://%s:%s%s RTSP/1.0\r\nCSeq: 1\r\n\r\n", host, port, path)
	_, err = conn.Write([]byte(msg))
	if err != nil {
		return 0, ""
	}

	buf := make([]byte, 2048)
	conn.SetReadDeadline(time.Now().Add(2000 * time.Millisecond))
	n, err := conn.Read(buf)
	if err != nil && err != io.EOF {
		return 0, ""
	}

	resp := string(buf[:n])

	if bytes.Contains(buf[:n], []byte("RTSP/1.0 200")) {
		return 200, resp
	}
	if bytes.Contains(buf[:n], []byte("RTSP/1.0 401")) {
		return 401, resp
	}
	if bytes.Contains(buf[:n], []byte("RTSP/1.0 404")) {
		return 404, resp
	}

	return 500, resp
}
