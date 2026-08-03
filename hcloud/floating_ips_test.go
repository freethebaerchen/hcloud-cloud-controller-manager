package hcloud

import (
	"net"
	"net/netip"
	"strings"
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

	t.Run("network address defaults to ::1", func(t *testing.T) {
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}
		base := net.ParseIP("2a01:4f8:1c17:a025::")
		assert.Equal(t, "2a01:4f8:1c17:a025::1", getIPv6AddressForIngress(svc, base))
	})

	t.Run("non-network address used as-is", func(t *testing.T) {
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}
		base := net.ParseIP("2a01:4f8:1c17:a025::2")
		assert.Equal(t, "2a01:4f8:1c17:a025::2", getIPv6AddressForIngress(svc, base))
	})
}

func Test_autoAllocateIPv6(t *testing.T) {
	base := net.ParseIP("2a01:4f8:1c17:a025::")

	assertValidDerived := func(t *testing.T, addr string) {
		t.Helper()
		ip := net.ParseIP(addr)
		if !assert.NotNil(t, ip, "derived address %q must be a valid IPv6", addr) {
			return
		}
		assert.Nil(t, ip.To4())
		derived, ok := netip.AddrFromSlice(ip)
		assert.True(t, ok)
		assert.True(t, netip.MustParsePrefix("2a01:4f8:1c17:a025::/64").Contains(derived), "derived address %q must be inside the /64", addr)
	}

	t.Run("different UIDs produce different addresses", func(t *testing.T) {
		svc1 := &corev1.Service{ObjectMeta: metav1.ObjectMeta{UID: "uid-aaaa"}}
		svc2 := &corev1.Service{ObjectMeta: metav1.ObjectMeta{UID: "uid-bbbb"}}
		addr1 := autoAllocateIPv6(svc1, base)
		addr2 := autoAllocateIPv6(svc2, base)
		assert.NotEqual(t, addr1, addr2)
		assertValidDerived(t, addr1)
		assertValidDerived(t, addr2)
	})

	t.Run("same UID produces same address", func(t *testing.T) {
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{UID: "uid-cccc"}}
		addr1 := autoAllocateIPv6(svc, base)
		addr2 := autoAllocateIPv6(svc, base)
		assert.Equal(t, addr1, addr2)
		assertValidDerived(t, addr1)
	})

	t.Run("returns ::1 when no base", func(t *testing.T) {
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{UID: "uid-aaaa"}}
		assert.Equal(t, "::1", autoAllocateIPv6(svc, nil))
	})

	t.Run("returns ::1 when base is ipv4", func(t *testing.T) {
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{UID: "uid-aaaa"}}
		base4 := net.ParseIP("1.2.3.4")
		assert.Equal(t, "::1", autoAllocateIPv6(svc, base4))
	})

	t.Run("preserves non-network base as-is", func(t *testing.T) {
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{UID: "uid-aaaa"}}
		base2 := net.ParseIP("2a01:4f8:1c17:a025::2")
		assert.Equal(t, "2a01:4f8:1c17:a025::2", autoAllocateIPv6(svc, base2))
	})

	t.Run("host suffix is a valid hextet", func(t *testing.T) {
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{UID: "uid-aaaa"}}
		addr := autoAllocateIPv6(svc, base)
		assert.Contains(t, addr, "2a01:4f8:1c17:a025::")
		suffix := strings.TrimPrefix(addr, "2a01:4f8:1c17:a025::")
		assert.LessOrEqual(t, len(suffix), 4)
		assert.NotEqual(t, "", suffix)
	})
}

func Test_getIPv6AddressForIngress_autoAllocate(t *testing.T) {
	base := net.ParseIP("2a01:4f8:1c17:a025::")

	t.Run("auto-allocate overrides ipv6-address annotation", func(t *testing.T) {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				UID: "test-uid-override",
				Annotations: map[string]string{
					string(annotation.FIPIPv6AutoAllocate): "true",
					string(annotation.FIPIPv6Address):      "2001:db8::1",
				},
			},
		}
		got := getIPv6AddressForIngress(svc, base)
		// Should NOT be "2001:db8::1" - auto-allocate wins.
		assert.NotEqual(t, "2001:db8::1", got)
		assert.Contains(t, got, "2a01:4f8:1c17:a025::")
	})

	t.Run("auto-allocate disabled falls back to annotation", func(t *testing.T) {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				UID: "test-uid-override",
				Annotations: map[string]string{
					string(annotation.FIPIPv6Address): "2001:db8::1",
				},
			},
		}
		got := getIPv6AddressForIngress(svc, base)
		assert.Equal(t, "2001:db8::1", got)
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

func Test_buildIngressFromFIPsOnly_autoAllocate(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			UID:         "svc-auto-test",
			Annotations: map[string]string{string(annotation.FIPIPv6AutoAllocate): "true"},
		},
	}

	fip4 := &hcloud.FloatingIP{IP: net.ParseIP("1.2.3.4")}
	fip6 := &hcloud.FloatingIP{IP: net.ParseIP("2a01:4f8:1c17:a025::")}

	ingress := buildIngressFromFIPsOnly([]*hcloud.FloatingIP{fip4, fip6, nil}, svc)
	if !assert.Len(t, ingress, 2) {
		return
	}

	// IPv6 should be derived from auto-allocation, not the default ::1.
	assert.Contains(t, ingress[0].IP, "2a01:4f8:1c17:a025::")
	assert.NotEqual(t, "2a01:4f8:1c17:a025::1", ingress[0].IP)
	assert.Equal(t, "1.2.3.4", ingress[1].IP)
}
