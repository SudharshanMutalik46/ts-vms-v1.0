package sfu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

type Client struct {
	BaseURL      string
	SharedSecret string
	HTTPClient   *http.Client
}

func NewClient(baseURL, secret string) *Client {
	return &Client{
		BaseURL:      baseURL,
		SharedSecret: secret,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body interface{}, out interface{}) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, &buf)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Auth", c.SharedSecret)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var bodySample string
		if resp.ContentLength != 0 {
			b := make([]byte, 512)
			n, _ := resp.Body.Read(b)
			bodySample = string(b[:n])
		}
		return fmt.Errorf("SFU error: status=%d, body=%s", resp.StatusCode, bodySample)
	}

	if out != nil {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("[DEBUG] SFU Client Response (path=%s): %s", path, string(bodyBytes))
		return json.NewDecoder(bytes.NewReader(bodyBytes)).Decode(out)
	}

	return nil
}

// Mediasoup Signaling Wrapper Functions

func (c *Client) GetRouterRtpCapabilities(ctx context.Context, roomID string) (json.RawMessage, error) {
	var caps json.RawMessage
	err := c.do(ctx, "GET", "/rooms/"+roomID+"/rtp-capabilities", nil, &caps)
	return caps, err
}

func (c *Client) CreateWebRtcTransport(ctx context.Context, roomID string) (json.RawMessage, error) {
	var transport json.RawMessage
	err := c.do(ctx, "POST", "/rooms/"+roomID+"/transports/webrtc", nil, &transport)
	return transport, err
}

func (c *Client) ConnectWebRtcTransport(ctx context.Context, roomID, transportID string, dtlsParameters json.RawMessage) error {
	body := map[string]interface{}{"dtlsParameters": dtlsParameters}
	return c.do(ctx, "POST", "/rooms/"+roomID+"/transports/"+transportID+"/connect", body, nil)
}

func (c *Client) Produce(ctx context.Context, roomID, transportID string, kind string, rtpParameters json.RawMessage) (string, error) {
	body := map[string]interface{}{
		"kind":          kind,
		"rtpParameters": rtpParameters,
	}
	var resp struct {
		ID string `json:"id"`
	}
	err := c.do(ctx, "POST", "/rooms/"+roomID+"/transports/"+transportID+"/produce", body, &resp)
	return resp.ID, err
}

func (c *Client) Consume(ctx context.Context, roomID, transportID, producerID string, rtpCapabilities json.RawMessage) (json.RawMessage, error) {
	body := map[string]interface{}{
		"producerId":      producerID,
		"rtpCapabilities": rtpCapabilities,
	}
	var consumer json.RawMessage
	err := c.do(ctx, "POST", "/rooms/"+roomID+"/transports/"+transportID+"/consume", body, &consumer)
	return consumer, err
}

func (c *Client) ResumeConsumer(ctx context.Context, roomID, transportID, consumerID string) error {
	return c.do(ctx, "POST", "/rooms/"+roomID+"/transports/"+transportID+"/consumers/"+consumerID+"/resume", nil, nil)
}

func (c *Client) JoinRoom(ctx context.Context, roomID, sessionID string) error {
	body := map[string]string{"sessionId": sessionID}
	return c.do(ctx, "POST", "/rooms/"+roomID+"/join", body, nil)
}

type IngestResponse struct {
	IP   string `json:"ip"`
	Port int32  `json:"port"`
	SSRC uint32 `json:"ssrc"`
	PT   uint32 `json:"pt"`
}

func (c *Client) PrepareIngest(ctx context.Context, roomID string) (*IngestResponse, error) {
	var resp IngestResponse
	err := c.do(ctx, "POST", "/rooms/"+roomID+"/ingest", nil, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) LeaveRoom(ctx context.Context, roomID string) error {
	return c.do(ctx, "POST", "/sessions/leave", map[string]string{"roomId": roomID}, nil)
}
