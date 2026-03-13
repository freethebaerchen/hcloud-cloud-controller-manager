package hcops

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"

	"github.com/hetznercloud/hcloud-cloud-controller-manager/internal/annotation"
	"github.com/hetznercloud/hcloud-cloud-controller-manager/internal/metrics"
	"github.com/hetznercloud/hcloud-cloud-controller-manager/internal/providerid"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// LabelFloatingIPType identifies the type of a Floating IP (ipv4 or ipv6) when
// a service has multiple Floating IPs.
const LabelFloatingIPType = "hcloud-ccm/floating-ip-type"

// LabelCCMManaged marks a Floating IP as managed by the Cloud Controller Manager,
// so the CCM can identify existing FIPs and will not create a new one of the same type.
const LabelCCMManaged = "hcloud-ccm/managed"

type HCloudFloatingIPClient interface {
	AllWithOpts(ctx context.Context, opts hcloud.FloatingIPListOpts) ([]*hcloud.FloatingIP, error)
	Create(ctx context.Context, opts hcloud.FloatingIPCreateOpts) (hcloud.FloatingIPCreateResult, *hcloud.Response, error)
	Delete(ctx context.Context, floatingIP *hcloud.FloatingIP) (*hcloud.Response, error)
	GetByID(ctx context.Context, id int64) (*hcloud.FloatingIP, *hcloud.Response, error)
	Assign(ctx context.Context, floatingIP *hcloud.FloatingIP, server *hcloud.Server) (*hcloud.Action, *hcloud.Response, error)
	Unassign(ctx context.Context, floatingIP *hcloud.FloatingIP) (*hcloud.Action, *hcloud.Response, error)
}

type HCloudServerClient interface {
	GetByID(ctx context.Context, id int64) (*hcloud.Server, *hcloud.Response, error)
}

type FloatingIPOps struct {
	FIPClient     HCloudFloatingIPClient
	ActionClient  HCloudActionClient
	ServerClient  HCloudServerClient
	Recorder      record.EventRecorder
}

func fipTypeLabel(typ hcloud.FloatingIPType) string {
	return string(typ)
}

func (f *FloatingIPOps) GetByK8SServiceUIDAndType(ctx context.Context, svc *corev1.Service, typ hcloud.FloatingIPType) (*hcloud.FloatingIP, error) {
	const op = "hcops/FloatingIPOps.GetByK8SServiceUIDAndType"
	metrics.OperationCalled.WithLabelValues(op).Inc()

	selector := fmt.Sprintf("%s=%s,%s=%s", LabelServiceUID, svc.ObjectMeta.UID, LabelFloatingIPType, fipTypeLabel(typ))
	opts := hcloud.FloatingIPListOpts{
		ListOpts: hcloud.ListOpts{LabelSelector: selector},
	}
	list, err := f.FIPClient.AllWithOpts(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("%s: api error: %w", op, err)
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("%s: %w", op, ErrNotFound)
	}
	if len(list) > 1 {
		return nil, fmt.Errorf("%s: %w", op, ErrNonUniqueResult)
	}
	return list[0], nil
}

func (f *FloatingIPOps) GetAllByK8SServiceUID(ctx context.Context, svc *corev1.Service) ([]*hcloud.FloatingIP, error) {
	const op = "hcops/FloatingIPOps.GetAllByK8SServiceUID"
	metrics.OperationCalled.WithLabelValues(op).Inc()

	opts := hcloud.FloatingIPListOpts{
		ListOpts: hcloud.ListOpts{
			LabelSelector: fmt.Sprintf("%s=%s", LabelServiceUID, svc.ObjectMeta.UID),
		},
	}
	list, err := f.FIPClient.AllWithOpts(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("%s: api error: %w", op, err)
	}
	return list, nil
}

func (f *FloatingIPOps) Create(ctx context.Context, location string, svc *corev1.Service, typ hcloud.FloatingIPType) (*hcloud.FloatingIP, error) {
	const op = "hcops/FloatingIPOps.Create"
	metrics.OperationCalled.WithLabelValues(op).Inc()

	opts := hcloud.FloatingIPCreateOpts{
		Type:         typ,
		HomeLocation: &hcloud.Location{Name: location},
		Labels: map[string]string{
			LabelServiceUID:     string(svc.ObjectMeta.UID),
			LabelFloatingIPType: fipTypeLabel(typ),
			LabelCCMManaged:     "true",
		},
	}
	result, _, err := f.FIPClient.Create(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if result.Action != nil {
		if err := f.ActionClient.WaitFor(ctx, result.Action); err != nil {
			return nil, fmt.Errorf("%s: wait for create action: %w", op, err)
		}
	}
	return result.FloatingIP, nil
}

func (f *FloatingIPOps) Delete(ctx context.Context, floatingIP *hcloud.FloatingIP) error {
	const op = "hcops/FloatingIPOps.Delete"
	metrics.OperationCalled.WithLabelValues(op).Inc()

	_, err := f.FIPClient.Delete(ctx, floatingIP)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

func (f *FloatingIPOps) ReconcileAssignment(
	ctx context.Context, floatingIPs []*hcloud.FloatingIP, svc *corev1.Service, nodes []*corev1.Node,
) error {
	const op = "hcops/FloatingIPOps.ReconcileAssignment"
	metrics.OperationCalled.WithLabelValues(op).Inc()

	if len(floatingIPs) == 0 {
		return nil
	}
	first := floatingIPs[0]
	if first.HomeLocation == nil || first.HomeLocation.NetworkZone == "" {
		return fmt.Errorf("%s: floating IP has no home network zone", op)
	}
	homeZone := first.HomeLocation.NetworkZone

	// Build list of (node, server) for hcloud nodes in the same network zone that are Ready.
	type candidate struct {
		node   *corev1.Node
		server *hcloud.Server
	}
	var candidates []candidate
	for _, node := range nodes {
		serverID, isCloud, err := providerid.ToServerID(node.Spec.ProviderID)
		if err != nil {
			if errors.As(err, new(*providerid.UnkownPrefixError)) {
				continue
			}
			return fmt.Errorf("%s: %w", op, err)
		}
		if !isCloud {
			continue
		}
		server, _, err := f.ServerClient.GetByID(ctx, serverID)
		if err != nil {
			return fmt.Errorf("%s: get server %d: %w", op, serverID, err)
		}
		if server == nil {
			continue
		}
		if server.Datacenter == nil || server.Datacenter.Location.NetworkZone != homeZone {
			continue
		}
		if !isNodeReady(node) {
			continue
		}
		candidates = append(candidates, candidate{node: node, server: server})
	}

	if len(candidates) == 0 {
		klog.InfoS("no Ready nodes in same network zone for Floating IP", "op", op, "service", svc.Name, "networkZone", homeZone)
		f.Recorder.Eventf(svc, corev1.EventTypeWarning, "NoFIPTarget",
			"No Ready node in network zone %s for Floating IP assignment", homeZone)
		return nil
	}

	var target *hcloud.Server
	for _, c := range candidates {
		for _, fip := range floatingIPs {
			if fip.Server != nil && fip.Server.ID == c.server.ID {
				target = c.server
				break
			}
		}
		if target != nil {
			break
		}
	}
	if target == nil {
		target = candidates[0].server
	}

	for _, floatingIP := range floatingIPs {
		if floatingIP.Server != nil && floatingIP.Server.ID == target.ID {
			continue
		}
		if floatingIP.Server != nil {
			klog.InfoS("unassign Floating IP from server", "op", op, "floatingIPID", floatingIP.ID, "serverID", floatingIP.Server.ID)
			a, _, err := f.FIPClient.Unassign(ctx, floatingIP)
			if err != nil {
				return fmt.Errorf("%s: unassign: %w", op, err)
			}
			if err := f.ActionClient.WaitFor(ctx, a); err != nil {
				return fmt.Errorf("%s: wait for unassign: %w", op, err)
			}
		}
		klog.InfoS("assign Floating IP to server", "op", op, "floatingIPID", floatingIP.ID, "serverID", target.ID)
		a, _, err := f.FIPClient.Assign(ctx, floatingIP, target)
		if err != nil {
			return fmt.Errorf("%s: assign: %w", op, err)
		}
		if err := f.ActionClient.WaitFor(ctx, a); err != nil {
			return fmt.Errorf("%s: wait for assign: %w", op, err)
		}
	}
	return nil
}

func isNodeReady(node *corev1.Node) bool {
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func FloatingIPEnabled(svc *corev1.Service) bool {
	if enabled, err := annotation.FIPEnabled.BoolFromService(svc); err == nil && enabled {
		return true
	}
	if v, err := annotation.FIPIPv4.BoolFromService(svc); err == nil && v {
		return true
	}
	if v, err := annotation.FIPIPv6.BoolFromService(svc); err == nil && v {
		return true
	}
	return false
}

func RequestedFIPTypes(svc *corev1.Service) []hcloud.FloatingIPType {
	var out []hcloud.FloatingIPType
	ipv4, _ := annotation.FIPIPv4.BoolFromService(svc)
	ipv6, _ := annotation.FIPIPv6.BoolFromService(svc)
	enabled, _ := annotation.FIPEnabled.BoolFromService(svc)
	if ipv4 {
		out = append(out, hcloud.FloatingIPTypeIPv4)
	}
	if ipv6 {
		out = append(out, hcloud.FloatingIPTypeIPv6)
	}
	if len(out) == 0 && enabled {
		out = append(out, hcloud.FloatingIPTypeIPv4)
	}
	return out
}

func FloatingIPLocation(svc *corev1.Service, defaultLocation string) (string, bool) {
	if v, ok := annotation.FIPLocation.StringFromService(svc); ok && v != "" {
		return v, true
	}
	if defaultLocation != "" {
		return defaultLocation, true
	}
	return "", false
}

func (f *FloatingIPOps) RecordEvent(svc *corev1.Service, eventType, reason, message string) {
	if f.Recorder != nil {
		f.Recorder.Eventf(svc, eventType, reason, "%s", message)
	}
}
