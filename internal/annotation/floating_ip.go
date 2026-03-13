package annotation

const (
	// FIPEnabled enables Floating IP management for this Service. When true
	// (and no ipv4/ipv6 specified), an IPv4 Floating IP is created. Prefer
	// using FIPIPv4 and FIPIPv6 to choose type(s).
	//
	// Type: bool
	// Default: false
	FIPEnabled Name = "floating-ip.hetzner.cloud/enabled"

	// FIPIPv4 requests an IPv4 Floating IP. When true with FIPIPv6, both are
	// created and always attached to the same node.
	//
	// Type: bool
	// Default: false
	FIPIPv4 Name = "floating-ip.hetzner.cloud/ipv4"

	// FIPIPv6 requests an IPv6 Floating IP. When true with FIPIPv4, both are
	// created and always attached to the same node.
	//
	// Type: bool
	// Default: false
	FIPIPv6 Name = "floating-ip.hetzner.cloud/ipv6"

	// FIPIPv6Address is the IPv6 address to use in load balancer ingress when
	// using Floating IPs only (e.g. load balancer disabled). If unset or
	// invalid, "::1" is used. Annotation values must be strings.
	//
	// Type: string
	// Default: "::1"
	FIPIPv6Address Name = "floating-ip.hetzner.cloud/ipv6-address"

	// FIPLocation is the Hetzner location for the Floating IP (e.g. nbg1,
	// fsn1, hel1). Required when FIP is enabled, or use the default from
	// HCLOUD_FLOATING_IP_LOCATION. Floating IPs can only be attached to
	// servers in this location.
	//
	// Type: string
	FIPLocation Name = "floating-ip.hetzner.cloud/location"

	// FIPPublicIP is the public IP address of the Floating IP. Set by the
	// Cloud Controller Manager.
	//
	// Type: string
	// Read-only: true
	FIPPublicIP Name = "floating-ip.hetzner.cloud/ip"
)
