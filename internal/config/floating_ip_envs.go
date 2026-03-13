package config

const (
	// hcloudFloatingIPLocation is the default Hetzner location for Floating IPs
	// created via the floating-ip.hetzner.cloud/enabled annotation (e.g. nbg1, fsn1, hel1).
	// Can be overridden per-service with floating-ip.hetzner.cloud/location.
	//
	// Type: string
	hcloudFloatingIPLocation = "HCLOUD_FLOATING_IP_LOCATION"
)
