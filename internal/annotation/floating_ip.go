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

	// FIPLocation is the Hetzner location for the Floating IP (e.g. nbg1,
	// fsn1, hel1). Required when FIP is enabled, or use the default from
	// HCLOUD_FLOATING_IP_LOCATION. Floating IPs can only be attached to
	// servers in this location.
	//
	// Type: string
	FIPLocation Name = "floating-ip.hetzner.cloud/location"

	// FIPName is the name to assign to the Floating IP. Used as a fallback
	// when the type-specific annotation (FIPNameIPv4 / FIPNameIPv6) is not set.
	// If neither is set, no name is assigned and Hetzner will use the IP
	// address as the name.
	//
	// Type: string
	// Default: ""
	FIPName Name = "floating-ip.hetzner.cloud/name"

	// FIPNameIPv4 is the name to assign to the IPv4 Floating IP. Overrides
	// the generic FIPName annotation.
	//
	// Type: string
	FIPNameIPv4 Name = "floating-ip.hetzner.cloud/name-ipv4"

	// FIPNameIPv6 is the name to assign to the IPv6 Floating IP. Overrides
	// the generic FIPName annotation.
	//
	// Type: string
	FIPNameIPv6 Name = "floating-ip.hetzner.cloud/name-ipv6"

	// FIPPublicIP is the public IP address of the Floating IP. Set by the
	// Cloud Controller Manager.
	//
	// Type: string
	// Read-only: true
	FIPPublicIP Name = "floating-ip.hetzner.cloud/ip"
)
