package e2b

// AllTraffic is the CIDR range representing all traffic.
// Use in DenyOut to block all outbound traffic by default,
// then selectively allow hosts via AllowOut.
const AllTraffic = "0.0.0.0/0"

// NetworkConfig configures outbound network access for a sandbox.
type NetworkConfig struct {
	// AllowOut lists IP addresses, CIDR blocks, or domain names
	// permitted for outbound traffic.
	// Domains support wildcards (e.g. "*.example.com").
	// Domains only filter HTTP (port 80) and TLS (port 443) traffic.
	// When using domains, DenyOut must contain "0.0.0.0/0".
	AllowOut []string `json:"allowOut,omitempty"`

	// DenyOut lists IP addresses and CIDR blocks to block.
	// Domains are NOT supported in deny lists (only CIDRs).
	// Use "0.0.0.0/0" (or the AllTraffic constant) to deny all traffic.
	DenyOut []string `json:"denyOut,omitempty"`

	// Rules defines per-host request transforms (e.g. header injection)
	// on outbound HTTP/HTTPS requests.
	// The host must also appear in AllowOut for egress to be permitted.
	Rules map[string][]RequestRule `json:"rules,omitempty"`

	// EgressProxy configures a SOCKS5 proxy for outbound traffic.
	// Requires team-level feature enablement.
	EgressProxy *EgressProxyConfig `json:"egressProxy,omitempty"`

	// AllowPublicTraffic controls whether sandbox URLs are publicly
	// accessible. Create-only. Defaults to true.
	// Setting to false requires Secure: true in SandboxConfig,
	// and the sandbox will return a TrafficAccessToken in the
	// create response.
	AllowPublicTraffic *bool `json:"allowPublicTraffic,omitempty"`

	// MaskRequestHost is a hostname that replaces the original request
	// host on outbound requests. Create-only.
	// Supports ${PORT} variable substitution
	// (e.g. "custom-host.example.com:${PORT}").
	MaskRequestHost string `json:"maskRequestHost,omitempty"`
}

// NetworkUpdateConfig is the request body for updating network configuration
// on a running sandbox via PUT /sandboxes/{id}/network.
// All fields are optional; omitted fields are cleared on the server.
// This replaces the entire mutable config, it does not merge.
type NetworkUpdateConfig struct {
	// AllowOut lists IP addresses, CIDR blocks, or domain names
	// permitted for outbound traffic.
	AllowOut []string `json:"allowOut,omitempty"`

	// DenyOut lists IP addresses and CIDR blocks to block.
	DenyOut []string `json:"denyOut,omitempty"`

	// Rules defines per-host request transforms. Replaces all existing rules.
	Rules map[string][]RequestRule `json:"rules,omitempty"`

	// EgressProxy configures a SOCKS5 proxy for outbound traffic.
	// Requires team-level feature enablement.
	EgressProxy *EgressProxyConfig `json:"egressProxy,omitempty"`

	// AllowInternetAccess controls whether the sandbox can access
	// the internet. When false, equivalent to DenyOut: ["0.0.0.0/0"].
	AllowInternetAccess *bool `json:"allow_internet_access,omitempty"`
}

// EgressProxyConfig configures a SOCKS5 proxy for outbound traffic.
type EgressProxyConfig struct {
	// Address is the SOCKS5 proxy address in host:port format
	// (e.g. "proxy.example.com:1080").
	Address string `json:"address"`

	// Username is the optional SOCKS5 username (RFC 1929, max 255 bytes).
	Username string `json:"username,omitempty"`

	// Password is the optional SOCKS5 password (RFC 1929, max 255 bytes).
	Password string `json:"password,omitempty"`
}

// RequestRule defines a transform to apply on matching outbound requests.
type RequestRule struct {
	Transform RequestTransform `json:"transform"`
}

// RequestTransform specifies the modifications to apply to a request.
type RequestTransform struct {
	// Headers are injected into matching HTTP/HTTPS requests.
	// An existing header with the same name is replaced.
	Headers map[string]string `json:"headers"`
}

// DenyAllOutbound returns a NetworkConfig that blocks all outbound traffic.
func DenyAllOutbound() *NetworkConfig {
	return &NetworkConfig{
		DenyOut: []string{AllTraffic},
	}
}

// AllowOutbound returns a NetworkConfig allowing only the specified hosts.
// All other outbound traffic is denied via 0.0.0.0/0.
func AllowOutbound(hosts ...string) *NetworkConfig {
	return &NetworkConfig{
		DenyOut:  []string{AllTraffic},
		AllowOut: hosts,
	}
}

// WithRequestTransform adds a per-host header injection rule to the config.
// The host must also be included in AllowOut for traffic to be permitted.
func (n *NetworkConfig) WithRequestTransform(host string, headers map[string]string) *NetworkConfig {
	if n.Rules == nil {
		n.Rules = make(map[string][]RequestRule)
	}
	n.Rules[host] = append(n.Rules[host], RequestRule{
		Transform: RequestTransform{Headers: headers},
	})
	return n
}

// Bool returns a pointer to the given bool value.
// Useful for setting optional boolean fields like AllowPublicTraffic.
func Bool(v bool) *bool {
	return &v
}
