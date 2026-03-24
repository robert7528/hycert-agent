package api

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hysp/hycert-agent/internal/model"
)

// Client talks to hycert-api Agent endpoints.
type Client struct {
	baseURL    string
	token      string
	agentID    string
	httpClient *http.Client
}

// NewClient creates an API client.
func NewClient(baseURL, token, agentID, proxy string, insecureSkipVerify bool) *Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	}
	if proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}
	if insecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		agentID: agentID,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

// GetDeployments fetches deployments assigned to this agent (identified by X-Agent-ID header).
func (c *Client) GetDeployments() ([]model.AgentDeployment, error) {
	u := fmt.Sprintf("%s/api/v1/agent/cert/deployments", c.baseURL)

	var resp model.APIResponse[[]model.AgentDeployment]
	if err := c.doJSON("GET", u, nil, &resp); err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, c.apiError(resp.Error)
	}
	return resp.Data, nil
}

// Register sends the agent registration to the server (upsert).
func (c *Client) Register(req model.RegisterRequest) (*model.RegisterResponse, error) {
	u := fmt.Sprintf("%s/api/v1/agent/cert/register", c.baseURL)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	var resp model.APIResponse[model.RegisterResponse]
	if err := c.doJSON("POST", u, body, &resp); err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, c.apiError(resp.Error)
	}
	return &resp.Data, nil
}

// DownloadOptions holds query parameters for certificate download.
type DownloadOptions struct {
	Format     string
	IncludeKey bool
	Password   string
	Alias      string
}

// DownloadCert downloads a certificate in the specified format.
func (c *Client) DownloadCert(certID uint, opts DownloadOptions) (*model.DownloadResult, error) {
	u := fmt.Sprintf("%s/api/v1/agent/cert/certificates/%d/download?format=%s",
		c.baseURL, certID, url.QueryEscape(opts.Format))
	if opts.IncludeKey {
		u += "&include_key=true"
	}
	if opts.Password != "" {
		u += "&password=" + url.QueryEscape(opts.Password)
	}
	if opts.Alias != "" {
		u += "&alias=" + url.QueryEscape(opts.Alias)
	}

	var resp model.APIResponse[model.DownloadResult]
	if err := c.doJSON("GET", u, nil, &resp); err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, c.apiError(resp.Error)
	}
	return &resp.Data, nil
}

// UpdateDeployStatus reports deployment result back to the server.
func (c *Client) UpdateDeployStatus(deployID uint, req model.UpdateStatusRequest) error {
	u := fmt.Sprintf("%s/api/v1/agent/cert/deployments/%d/status", c.baseURL, deployID)

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	var resp model.APIResponse[json.RawMessage]
	if err := c.doJSON("PUT", u, body, &resp); err != nil {
		return err
	}
	if !resp.Success {
		return c.apiError(resp.Error)
	}
	return nil
}

func (c *Client) doJSON(method, url string, body []byte, result any) error {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	if c.agentID != "" {
		req.Header.Set("X-Agent-ID", c.agentID)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}

	if err := json.Unmarshal(data, result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) apiError(e *model.APIError) error {
	if e == nil {
		return fmt.Errorf("unknown API error")
	}
	return fmt.Errorf("API error [%s]: %s", e.Code, e.Message)
}
