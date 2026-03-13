package hcops_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hetznercloud/hcloud-cloud-controller-manager/internal/annotation"
	"github.com/hetznercloud/hcloud-cloud-controller-manager/internal/hcops"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

func TestFloatingIPEnabled(t *testing.T) {
	tests := []struct {
		name    string
		svc     *corev1.Service
		enabled bool
	}{
		{
			name:    "annotation missing",
			svc:     &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: nil}},
			enabled: false,
		},
		{
			name: "enabled false",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{string(annotation.FIPEnabled): "false"},
				},
			},
			enabled: false,
		},
		{
			name: "enabled true",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{string(annotation.FIPEnabled): "true"},
				},
			},
			enabled: true,
		},
		{
			name: "ipv4 true",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{string(annotation.FIPIPv4): "true"},
				},
			},
			enabled: true,
		},
		{
			name: "ipv6 true",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{string(annotation.FIPIPv6): "true"},
				},
			},
			enabled: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hcops.FloatingIPEnabled(tt.svc)
			assert.Equal(t, tt.enabled, got)
		})
	}
}

func TestRequestedFIPTypes(t *testing.T) {
	tests := []struct {
		name string
		svc  *corev1.Service
		want []hcloud.FloatingIPType
	}{
		{
			name: "none",
			svc:  &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: nil}},
			want: nil,
		},
		{
			name: "ipv4 only",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{string(annotation.FIPIPv4): "true"},
				},
			},
			want: []hcloud.FloatingIPType{hcloud.FloatingIPTypeIPv4},
		},
		{
			name: "ipv6 only",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{string(annotation.FIPIPv6): "true"},
				},
			},
			want: []hcloud.FloatingIPType{hcloud.FloatingIPTypeIPv6},
		},
		{
			name: "both ipv4 and ipv6",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						string(annotation.FIPIPv4): "true",
						string(annotation.FIPIPv6): "true",
					},
				},
			},
			want: []hcloud.FloatingIPType{hcloud.FloatingIPTypeIPv4, hcloud.FloatingIPTypeIPv6},
		},
		{
			name: "enabled only defaults to ipv4",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{string(annotation.FIPEnabled): "true"},
				},
			},
			want: []hcloud.FloatingIPType{hcloud.FloatingIPTypeIPv4},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hcops.RequestedFIPTypes(tt.svc)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFloatingIPLocation(t *testing.T) {
	tests := []struct {
		name           string
		svc            *corev1.Service
		defaultLoc     string
		location       string
		ok             bool
	}{
		{
			name:       "annotation set",
			svc:        &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{string(annotation.FIPLocation): "nbg1"}}},
			defaultLoc: "",
			location:   "nbg1",
			ok:         true,
		},
		{
			name:       "default used when annotation missing",
			svc:        &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: nil}},
			defaultLoc: "fsn1",
			location:   "fsn1",
			ok:         true,
		},
		{
			name:       "annotation overrides default",
			svc:        &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{string(annotation.FIPLocation): "hel1"}}},
			defaultLoc: "fsn1",
			location:   "hel1",
			ok:         true,
		},
		{
			name:       "empty when neither set",
			svc:        &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: nil}},
			defaultLoc: "",
			location:   "",
			ok:         false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loc, ok := hcops.FloatingIPLocation(tt.svc, tt.defaultLoc)
			assert.Equal(t, tt.location, loc)
			assert.Equal(t, tt.ok, ok)
		})
	}
}

func TestReconcileAssignment_UsesNetworkZone(t *testing.T) {
	ctx := context.Background()
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "default",
		},
	}

	fip := &hcloud.FloatingIP{
		ID: 1,
		HomeLocation: &hcloud.Location{
			Name:        "nbg1",
			NetworkZone: "eu-central",
		},
	}

	mockFIP := new(mockFloatingIPClient)
	mockServer := new(mockServerClient)
	mockAction := new(mockActionClient)

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node1",
		},
		Spec: corev1.NodeSpec{
			ProviderID: "hcloud://123",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	}

	server := &hcloud.Server{
		ID: 123,
		Datacenter: &hcloud.Datacenter{
			Location: &hcloud.Location{
				Name:        "fsn1",
				NetworkZone: "eu-central",
			},
		},
	}

	mockServer.On("GetByID", ctx, int64(123)).Return(server, &hcloud.Response{}, nil)
	mockFIP.On("Assign", ctx, fip, server).Return(&hcloud.Action{}, &hcloud.Response{}, nil)
	mockAction.On("WaitFor", ctx, mock.Anything).Return(nil)

	ops := &hcops.FloatingIPOps{
		FIPClient:    mockFIP,
		ActionClient: mockAction,
		ServerClient: mockServer,
	}

	err := ops.ReconcileAssignment(ctx, []*hcloud.FloatingIP{fip}, svc, []*corev1.Node{node})
	assert.NoError(t, err)

	mockFIP.AssertExpectations(t)
	mockServer.AssertExpectations(t)
	mockAction.AssertExpectations(t)
}

type mockFloatingIPClient struct {
	mock.Mock
}

func (m *mockFloatingIPClient) AllWithOpts(ctx context.Context, opts hcloud.FloatingIPListOpts) ([]*hcloud.FloatingIP, error) {
	args := m.Called(ctx, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*hcloud.FloatingIP), args.Error(1)
}

func (m *mockFloatingIPClient) Create(ctx context.Context, opts hcloud.FloatingIPCreateOpts) (hcloud.FloatingIPCreateResult, *hcloud.Response, error) {
	args := m.Called(ctx, opts)
	return args.Get(0).(hcloud.FloatingIPCreateResult), args.Get(1).(*hcloud.Response), args.Error(2)
}

func (m *mockFloatingIPClient) Delete(ctx context.Context, floatingIP *hcloud.FloatingIP) (*hcloud.Response, error) {
	args := m.Called(ctx, floatingIP)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*hcloud.Response), args.Error(1)
}

func (m *mockFloatingIPClient) GetByID(ctx context.Context, id int64) (*hcloud.FloatingIP, *hcloud.Response, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Get(1).(*hcloud.Response), args.Error(2)
	}
	return args.Get(0).(*hcloud.FloatingIP), args.Get(1).(*hcloud.Response), args.Error(2)
}

func (m *mockFloatingIPClient) Assign(ctx context.Context, floatingIP *hcloud.FloatingIP, server *hcloud.Server) (*hcloud.Action, *hcloud.Response, error) {
	args := m.Called(ctx, floatingIP, server)
	if args.Get(0) == nil {
		return nil, args.Get(1).(*hcloud.Response), args.Error(2)
	}
	return args.Get(0).(*hcloud.Action), args.Get(1).(*hcloud.Response), args.Error(2)
}

func (m *mockFloatingIPClient) Unassign(ctx context.Context, floatingIP *hcloud.FloatingIP) (*hcloud.Action, *hcloud.Response, error) {
	args := m.Called(ctx, floatingIP)
	if args.Get(0) == nil {
		return nil, args.Get(1).(*hcloud.Response), args.Error(2)
	}
	return args.Get(0).(*hcloud.Action), args.Get(1).(*hcloud.Response), args.Error(2)
}

type mockServerClient struct {
	mock.Mock
}

func (m *mockServerClient) GetByID(ctx context.Context, id int64) (*hcloud.Server, *hcloud.Response, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Get(1).(*hcloud.Response), args.Error(2)
	}
	return args.Get(0).(*hcloud.Server), args.Get(1).(*hcloud.Response), args.Error(2)
}

type mockActionClient struct {
	mock.Mock
}

func (m *mockActionClient) WaitFor(ctx context.Context, actions ...*hcloud.Action) error {
	args := m.Called(ctx, actions)
	return args.Error(0)
}

func TestFloatingIPOps_GetByK8SServiceUIDAndType(t *testing.T) {
	ctx := context.Background()
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{UID: "svc-uid-123"}}

	t.Run("not found", func(t *testing.T) {
		mockFIP := new(mockFloatingIPClient)
		mockFIP.On("AllWithOpts", ctx, mock.MatchedBy(func(opts hcloud.FloatingIPListOpts) bool {
			return opts.LabelSelector == "hcloud-ccm/service-uid=svc-uid-123,hcloud-ccm/floating-ip-type=ipv4"
		})).Return([]*hcloud.FloatingIP{}, nil)

		ops := &hcops.FloatingIPOps{FIPClient: mockFIP}
		_, err := ops.GetByK8SServiceUIDAndType(ctx, svc, hcloud.FloatingIPTypeIPv4)
		assert.True(t, errors.Is(err, hcops.ErrNotFound))
		mockFIP.AssertExpectations(t)
	})

	t.Run("found", func(t *testing.T) {
		fip := &hcloud.FloatingIP{ID: 42, IP: net.ParseIP("1.2.3.4")}
		mockFIP := new(mockFloatingIPClient)
		mockFIP.On("AllWithOpts", ctx, mock.Anything).Return([]*hcloud.FloatingIP{fip}, nil)

		ops := &hcops.FloatingIPOps{FIPClient: mockFIP}
		got, err := ops.GetByK8SServiceUIDAndType(ctx, svc, hcloud.FloatingIPTypeIPv4)
		assert.NoError(t, err)
		assert.Equal(t, fip, got)
		mockFIP.AssertExpectations(t)
	})

	t.Run("non-unique", func(t *testing.T) {
		mockFIP := new(mockFloatingIPClient)
		mockFIP.On("AllWithOpts", ctx, mock.Anything).Return([]*hcloud.FloatingIP{
			{ID: 1}, {ID: 2},
		}, nil)

		ops := &hcops.FloatingIPOps{FIPClient: mockFIP}
		_, err := ops.GetByK8SServiceUIDAndType(ctx, svc, hcloud.FloatingIPTypeIPv6)
		assert.True(t, errors.Is(err, hcops.ErrNonUniqueResult))
		mockFIP.AssertExpectations(t)
	})
}

func TestFloatingIPOps_GetAllByK8SServiceUID(t *testing.T) {
	ctx := context.Background()
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{UID: "svc-uid-123"}}

	t.Run("empty", func(t *testing.T) {
		mockFIP := new(mockFloatingIPClient)
		mockFIP.On("AllWithOpts", ctx, mock.MatchedBy(func(opts hcloud.FloatingIPListOpts) bool {
			return opts.LabelSelector == "hcloud-ccm/service-uid=svc-uid-123"
		})).Return([]*hcloud.FloatingIP{}, nil)

		ops := &hcops.FloatingIPOps{FIPClient: mockFIP}
		got, err := ops.GetAllByK8SServiceUID(ctx, svc)
		assert.NoError(t, err)
		assert.Empty(t, got)
		mockFIP.AssertExpectations(t)
	})

	t.Run("multiple", func(t *testing.T) {
		fips := []*hcloud.FloatingIP{
			{ID: 1, IP: net.ParseIP("1.2.3.4")},
			{ID: 2, IP: net.ParseIP("2001:db8::1")},
		}
		mockFIP := new(mockFloatingIPClient)
		mockFIP.On("AllWithOpts", ctx, mock.Anything).Return(fips, nil)

		ops := &hcops.FloatingIPOps{FIPClient: mockFIP}
		got, err := ops.GetAllByK8SServiceUID(ctx, svc)
		assert.NoError(t, err)
		assert.Equal(t, fips, got)
		mockFIP.AssertExpectations(t)
	})
}
