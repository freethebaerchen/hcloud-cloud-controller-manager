package hcloud

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/hetznercloud/hcloud-cloud-controller-manager/internal/annotation"
	"github.com/hetznercloud/hcloud-cloud-controller-manager/internal/hcops"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// FloatingIPOps defines the Floating IP operations used when the service has
// floating-ip.hetzner.cloud/ipv4 and/or ipv6 enabled. Optional; if nil, Floating IP logic is skipped.
type FloatingIPOps interface {
	GetByK8SServiceUIDAndType(ctx context.Context, svc *corev1.Service, typ hcloud.FloatingIPType) (*hcloud.FloatingIP, error)
	GetAllByK8SServiceUID(ctx context.Context, svc *corev1.Service) ([]*hcloud.FloatingIP, error)
	Create(ctx context.Context, location string, svc *corev1.Service, typ hcloud.FloatingIPType) (*hcloud.FloatingIP, error)
	Delete(ctx context.Context, floatingIP *hcloud.FloatingIP) error
	ReconcileAssignment(ctx context.Context, floatingIPs []*hcloud.FloatingIP, svc *corev1.Service, nodes []*corev1.Node) error
	RecordEvent(svc *corev1.Service, eventType, reason, message string)
}

func (l *loadBalancers) ensureFloatingIPs(ctx context.Context, op string, svc *corev1.Service, selectedNodes []*corev1.Node) ([]*hcloud.FloatingIP, error) {
	if l.fipOps == nil || !hcops.FloatingIPEnabled(svc) {
		return nil, nil
	}

	location, ok := hcops.FloatingIPLocation(svc, l.cfg.FloatingIPLocation)
	if !ok {
		l.fipOps.RecordEvent(svc, corev1.EventTypeWarning, "FloatingIPLocationMissing",
			"Floating IP is enabled but no location set. Set floating-ip.hetzner.cloud/location or HCLOUD_FLOATING_IP_LOCATION")
		return nil, nil
	}

	requestedTypes := hcops.RequestedFIPTypes(svc)
	klog.InfoS("requested Floating IP types", "op", op, "service", svc.Namespace+"/"+svc.Name, "types", requestedTypes)

	var fips []*hcloud.FloatingIP
	for _, typ := range requestedTypes {
		fip, getErr := l.fipOps.GetByK8SServiceUIDAndType(ctx, svc, typ)
		if getErr != nil && !errors.Is(getErr, hcops.ErrNotFound) {
			return nil, fmt.Errorf("%s: get floating IP %s: %w", op, typ, getErr)
		}
		if errors.Is(getErr, hcops.ErrNotFound) {
			var err error
			fip, err = l.fipOps.Create(ctx, location, svc, typ)
			if err != nil {
				return nil, fmt.Errorf("%s: create floating IP %s: %w", op, typ, err)
			}
		}
		fips = append(fips, fip)
	}

	if len(fips) == 0 {
		return nil, nil
	}

	if err := l.fipOps.ReconcileAssignment(ctx, fips, svc, selectedNodes); err != nil {
		return nil, fmt.Errorf("%s: reconcile floating IP assignment: %w", op, err)
	}

	// Refresh to ensure we return the current state after (re)assignment.
	refreshed, _ := l.fipOps.GetAllByK8SServiceUID(ctx, svc)
	return refreshed, nil
}

func (l *loadBalancers) updateFloatingIPAssignment(ctx context.Context, op string, svc *corev1.Service, selectedNodes []*corev1.Node) error {
	if l.fipOps == nil || !hcops.FloatingIPEnabled(svc) {
		return nil
	}
	if _, ok := hcops.FloatingIPLocation(svc, l.cfg.FloatingIPLocation); !ok {
		// Keep legacy behavior: don't emit the location-missing warning on Update.
		return nil
	}

	fips, err := l.fipOps.GetAllByK8SServiceUID(ctx, svc)
	if err != nil {
		return fmt.Errorf("%s: get floating IPs: %w", op, err)
	}
	if len(fips) == 0 {
		return nil
	}
	if err := l.fipOps.ReconcileAssignment(ctx, fips, svc, selectedNodes); err != nil {
		return fmt.Errorf("%s: reconcile floating IP assignment: %w", op, err)
	}
	return nil
}

func (l *loadBalancers) deleteFloatingIPs(ctx context.Context, op string, svc *corev1.Service) error {
	if l.fipOps == nil {
		return nil
	}

	fips, err := l.fipOps.GetAllByK8SServiceUID(ctx, svc)
	if err != nil {
		// Keep legacy behavior: ignore lookup errors during deletion.
		return nil
	}

	for _, fip := range fips {
		klog.InfoS("delete Floating IP", "op", op, "floatingIPID", fip.ID)
		if delErr := l.fipOps.Delete(ctx, fip); delErr != nil {
			return fmt.Errorf("%s: delete floating IP: %w", op, delErr)
		}
	}
	return nil
}

// getIPv6AddressForIngress returns the IPv6 address to use in ingress when using
// FIP-only (e.g. load balancer disabled).
//
// Behaviour:
//   - If floating-ip.hetzner.cloud/ipv6-address is a full IPv6 address, use it.
//   - If it is a suffix without ":", append it to the IPv6 block of base (e.g. base
//     "2a01:4f8:1c17:b0b0::" + suffix "1" => "2a01:4f8:1c17:b0b0::1").
//   - If the annotation is missing/invalid:
//     * use base (if non-nil IPv6), otherwise "::1".
func getIPv6AddressForIngress(svc *corev1.Service, base net.IP) string {
	const defaultIPv6 = "::1"

	v, ok := annotation.FIPIPv6Address.StringFromService(svc)
	if !ok || v == "" {
		if base != nil && base.To4() == nil {
			return base.String()
		}
		return defaultIPv6
	}

	// Annotation provides a full IPv6 address.
	if strings.Contains(v, ":") {
		ip := net.ParseIP(v)
		if ip == nil || ip.To4() != nil {
			if base != nil && base.To4() == nil {
				return base.String()
			}
			return defaultIPv6
		}
		return ip.String()
	}

	// Annotation is treated as suffix without ":"; append to base block if possible.
	if base != nil && base.To4() == nil {
		baseStr := base.String()
		if strings.HasSuffix(baseStr, "::") {
			candidate := baseStr + v
			if ip := net.ParseIP(candidate); ip != nil && ip.To4() == nil {
				return ip.String()
			}
		}
		// Fallback: use base as-is if we cannot construct a better address.
		return baseStr
	}

	return defaultIPv6
}

// buildIngressFromFIPsOnly returns LoadBalancerIngress entries for the given
// Floating IPs plus a configurable IPv6 entry (either derived from the FIP IPv6
// block and annotation or a sensible default). Used when load balancer is disabled.
func buildIngressFromFIPsOnly(fips []*hcloud.FloatingIP, svc *corev1.Service) []corev1.LoadBalancerIngress {
	ipMode := corev1.LoadBalancerIPModeVIP
	var ingress []corev1.LoadBalancerIngress

	// Determine base IPv6 block from the first IPv6 Floating IP, if any.
	var baseIPv6 net.IP
	for _, fip := range fips {
		if fip != nil && fip.IP != nil && fip.IP.To4() == nil {
			baseIPv6 = fip.IP
			break
		}
	}

	// First IPv6 entry (derived from base + annotation or default).
	ipv6Addr := getIPv6AddressForIngress(svc, baseIPv6)
	if ipv6Addr != "" {
		ingress = append(ingress, corev1.LoadBalancerIngress{
			IP:     ipv6Addr,
			IPMode: &ipMode,
		})
	}

	// Then append all IPv4 Floating IPs.
	for _, fip := range fips {
		if fip != nil && fip.IP != nil && fip.IP.To4() != nil {
			ingress = append(ingress, corev1.LoadBalancerIngress{
				IP:     fip.IP.String(),
				IPMode: &ipMode,
			})
		}
	}

	return ingress
}
