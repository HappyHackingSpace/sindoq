package vercel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/happyhackingspace/sindoq/internal/provider"
)

// vercelNetwork implements provider.Network for Vercel Sandbox.
type vercelNetwork struct {
	instance *Instance
	ports    map[int]*provider.PublishedPort
}

// PublishPort exposes a port publicly.
func (n *vercelNetwork) PublishPort(ctx context.Context, port int) (*provider.PublishedPort, error) {
	reqBody := map[string]any{
		"port": port,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/v1/sandbox/"+n.instance.id+"/ports", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	n.instance.provider.setHeaders(req)

	resp, err := n.instance.provider.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("publish port failed: %s - %s", resp.Status, string(bodyBytes))
	}

	var result struct {
		Port      int    `json:"port"`
		PublicURL string `json:"url"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	published := &provider.PublishedPort{
		LocalPort: port,
		PublicURL: result.PublicURL,
		Protocol:  "https",
	}

	if n.ports == nil {
		n.ports = make(map[int]*provider.PublishedPort)
	}
	n.ports[port] = published

	return published, nil
}

// GetPublicURL returns the public URL for an exposed port.
func (n *vercelNetwork) GetPublicURL(port int) (string, error) {
	if n.ports == nil {
		return "", fmt.Errorf("no ports published")
	}

	p, ok := n.ports[port]
	if !ok {
		return "", fmt.Errorf("port %d not published", port)
	}

	return p.PublicURL, nil
}

// ListPorts returns all published ports.
func (n *vercelNetwork) ListPorts(ctx context.Context) ([]*provider.PublishedPort, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/sandbox/"+n.instance.id+"/ports", nil)
	if err != nil {
		return nil, err
	}
	n.instance.provider.setHeaders(req)

	resp, err := n.instance.provider.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// Return cached ports
		ports := make([]*provider.PublishedPort, 0)
		for _, p := range n.ports {
			ports = append(ports, p)
		}
		return ports, nil
	}

	var result struct {
		Ports []struct {
			Port      int    `json:"port"`
			PublicURL string `json:"url"`
		} `json:"ports"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	ports := make([]*provider.PublishedPort, len(result.Ports))
	for i, p := range result.Ports {
		ports[i] = &provider.PublishedPort{
			LocalPort: p.Port,
			PublicURL: p.PublicURL,
			Protocol:  "https",
		}
	}

	return ports, nil
}

// UnpublishPort removes port exposure.
func (n *vercelNetwork) UnpublishPort(ctx context.Context, port int) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", baseURL+"/v1/sandbox/"+n.instance.id+"/ports/"+fmt.Sprintf("%d", port), nil)
	if err != nil {
		return err
	}
	n.instance.provider.setHeaders(req)

	resp, err := n.instance.provider.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if n.ports != nil {
		delete(n.ports, port)
	}

	return nil
}

// Ensure vercelNetwork implements provider.Network
var _ provider.Network = (*vercelNetwork)(nil)
