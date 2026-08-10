package hcloud

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

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
