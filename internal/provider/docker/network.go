package docker

import (
	"context"
	"fmt"

	"github.com/happyhackingspace/sindoq/internal/provider"
)

// dockerNetwork implements provider.Network for Docker containers.
type dockerNetwork struct {
	instance *Instance
	ports    map[int]*provider.PublishedPort
}

// PublishPort exposes a port publicly.
// Note: Docker requires port mappings at container creation time.
// This implementation provides a stub that returns an error.
func (n *dockerNetwork) PublishPort(ctx context.Context, port int) (*provider.PublishedPort, error) {
	// Docker requires port mappings to be specified at container creation time.
	// For dynamic port publishing, we would need to:
	// 1. Stop the container
	// 2. Commit it to an image
	// 3. Recreate with new port mappings
	// This is not practical for a sandbox environment.

	// Return an error indicating this limitation
	return nil, fmt.Errorf("docker provider requires ports to be configured at sandbox creation time. " +
		"Use CreateOptions.Metadata[\"ports\"] to specify port mappings")
}

// GetPublicURL returns the public URL for an exposed port.
func (n *dockerNetwork) GetPublicURL(port int) (string, error) {
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
// Note: This is a simplified implementation that returns cached ports.
func (n *dockerNetwork) ListPorts(ctx context.Context) ([]*provider.PublishedPort, error) {
	ports := make([]*provider.PublishedPort, 0)

	if n.ports != nil {
		for _, p := range n.ports {
			ports = append(ports, p)
		}
	}

	return ports, nil
}

// UnpublishPort removes port exposure.
func (n *dockerNetwork) UnpublishPort(ctx context.Context, port int) error {
	// Cannot dynamically unpublish ports in Docker
	return fmt.Errorf("docker provider does not support dynamic port unpublishing")
}

// Ensure dockerNetwork implements provider.Network
var _ provider.Network = (*dockerNetwork)(nil)
