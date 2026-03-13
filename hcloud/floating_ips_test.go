package hcloud

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hetznercloud/hcloud-cloud-controller-manager/internal/annotation"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

func Test_getIPv6AddressForIngress(t *testing.T) {
	t.Run("default when not set", func(t *testing.T) {
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}
		assert.Equal(t, "::1", getIPv6AddressForIngress(svc, nil))
	})

	t.Run("default when invalid", func(t *testing.T) {
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			string(annotation.FIPIPv6Address): "not-an-ip",
		}}}
		assert.Equal(t, "::1", getIPv6AddressForIngress(svc, nil))
	})

	t.Run("default when ipv4", func(t *testing.T) {
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			string(annotation.FIPIPv6Address): "1.2.3.4",
		}}}
		assert.Equal(t, "::1", getIPv6AddressForIngress(svc, nil))
	})

	t.Run("uses configured ipv6", func(t *testing.T) {
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			string(annotation.FIPIPv6Address): "2001:db8::1",
		}}}
		assert.Equal(t, "2001:db8::1", getIPv6AddressForIngress(svc, nil))
	})

	t.Run("suffix appended to base block", func(t *testing.T) {
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			string(annotation.FIPIPv6Address): "1",
		}}}
		base := net.ParseIP("2a01:4f8:1c17:b0b0::")
		assert.Equal(t, "2a01:4f8:1c17:b0b0::1", getIPv6AddressForIngress(svc, base))
	})
}

func Test_buildIngressFromFIPsOnly(t *testing.T) {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

	fip4 := &hcloud.FloatingIP{IP: net.ParseIP("1.2.3.4")}
	fip6 := &hcloud.FloatingIP{IP: net.ParseIP("2001:db8::2")}

	ingress := buildIngressFromFIPsOnly([]*hcloud.FloatingIP{fip4, fip6, nil}, svc)
	if !assert.Len(t, ingress, 2) {
		return
	}

	// First IPv6 derived from base (no annotation => use base as-is), then IPv4.
	assert.Equal(t, "2001:db8::2", ingress[0].IP)
	assert.Equal(t, "1.2.3.4", ingress[1].IP)

	assert.NotNil(t, ingress[0].IPMode)
	assert.NotNil(t, ingress[1].IPMode)
}
