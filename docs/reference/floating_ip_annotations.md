# Floating IP Annotations

When using a Service of type `LoadBalancer`, you can enable optional Floating IP management so that the Cloud Controller Manager creates a Hetzner Cloud Floating IP and attaches it to one of the service's target nodes (e.g. a node running an ingress controller pod). If that node is down, the Floating IP is reassigned to another Ready node in the **same location**. Floating IPs are location-bound (e.g. a Floating IP in `nbg1` can only be attached to servers in `nbg1`).

- Read-only annotations are set by the Cloud Controller Manager.
- The CCM only attaches the Floating IP to a node (external routing). It never publishes the Floating IP as the Service's IP — another controller (e.g. MetalLB) manages the in-cluster Service IP.
- **Kubernetes annotations accept only string values.** For boolean-like options use the strings `"true"` or `"false"`, not unquoted `true`/`false` (which would make Helm/Kubernetes reject the manifest).

| Name | Type | Default | Read-only | Description |
| --- | --- | --- | --- | --- |
| `floating-ip.hetzner.cloud/enabled` | `bool` | `false` | `No` | Enables Floating IP management; when `true` alone, an IPv4 Floating IP is created. Prefer `ipv4`/`ipv6` to choose type(s). |
| `floating-ip.hetzner.cloud/ipv4` | `bool` | `false` | `No` | Request an IPv4 Floating IP. Can be used together with `ipv6`; both are then always attached to the same node. |
| `floating-ip.hetzner.cloud/ipv6` | `bool` | `false` | `No` | Request an IPv6 Floating IP. Can be used together with `ipv4`; both are then always attached to the same node. |
| `floating-ip.hetzner.cloud/location` | `string` | `-` | `No` | Hetzner location for the Floating IP (e.g. `nbg1`, `fsn1`, `hel1`). Required when enabled unless `HCLOUD_FLOATING_IP_LOCATION` is set. The IP can only be attached to servers in this location. |
| `floating-ip.hetzner.cloud/name` | `string` | `-` | `No` | Name to assign to all Floating IPs. Falls back to IP address if unset. Overridden by `name-ipv4`/`name-ipv6`. |
| `floating-ip.hetzner.cloud/name-ipv4` | `string` | `-` | `No` | Name for the IPv4 Floating IP specifically. Overrides the generic `name` annotation. |
| `floating-ip.hetzner.cloud/name-ipv6` | `string` | `-` | `No` | Name for the IPv6 Floating IP specifically. Overrides the generic `name` annotation. |
| `floating-ip.hetzner.cloud/ip` | `string` | `-` | `Yes` | The public IP address of the Floating IP. Set by the Cloud Controller Manager. |

## Example

```yaml
annotations:
  floating-ip.hetzner.cloud/enabled: "true"   # must be string "true", not boolean true
  floating-ip.hetzner.cloud/location: fsn1
```

With explicit IPv4/IPv6 (both FIPs are created and attached to the same node):

```yaml
annotations:
  floating-ip.hetzner.cloud/enabled: "true"
  floating-ip.hetzner.cloud/ipv4: "true"
  floating-ip.hetzner.cloud/ipv6: "true"
  floating-ip.hetzner.cloud/location: "fsn1"
```

All annotation values must be strings (e.g. `"true"`). If an IPv6 Floating IP is not created, check that `floating-ip.hetzner.cloud/ipv6: "true"` is set and that the CCM image includes dual-stack FIP support; check CCM logs for errors.

## Labels on Floating IPs in Hetzner Cloud

The CCM tags every Floating IP it creates with labels in Hetzner Cloud so it can find existing FIPs and will not create a new one of the same type for the same service:

| Label | Description |
| --- | --- |
| `hcloud-ccm/service-uid` | Kubernetes Service UID this FIP belongs to. |
| `hcloud-ccm/floating-ip-type` | `ipv4` or `ipv6`. |
| `hcloud-ccm/managed` | Set to `true` for CCM-managed FIPs. |

Lookup is done by `hcloud-ccm/service-uid` and `hcloud-ccm/floating-ip-type`. If a matching FIP exists, the CCM reuses it and does not create another. To use an existing Floating IP that was created outside the CCM (e.g. in the Hetzner Cloud Console), add these labels to that FIP with the correct service UID and type (`ipv4` or `ipv6`); the CCM will then find and use it.
