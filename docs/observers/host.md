<!--- GENERATED BY gomplate from scripts/docs/observer-page.md.tmpl --->

# host

 Looks at the current host for listening network endpoints.
It uses the `/proc` filesystem and requires the `SYS_PTRACE` and
`DAC_READ_SEARCH` capabilities so that it can determine what processes own
the listening sockets.

It will look for all listening sockets on TCP and UDP over IPv4 and IPv6.


Observer Type: `host`

[Observer Source Code](https://github.com/signalfx/signalfx-agent/tree/master/internal/observers/host)

## Configuration

This observer has no configuration options.


## Endpoint Variables

The following fields are available on endpoints generated by this observer and
can be used in discovery rules.

| Name | Type | Description |
| ---  | ---  | ---         |
| `ip_address` | `string` | The IP address of the endpoint if the `host` is in the from of an IPv4 address |
| `network_port` | `string` | An alias for `port` |
| `discovered_by` | `string` | The observer that discovered this endpoint |
| `host` | `string` | The hostname/IP address of the endpoint |
| `id` | `string` |  |
| `name` | `string` | A observer assigned name of the endpoint |
| `port` | `integer` | The TCP/UDP port number of the endpoint |
| `port_type` | `string` | TCP or UDP |

## Dimensions

These dimensions are added to all metrics that are emitted for this service
endpoint.  These variables are also available to use as variables in discovery
rules.

| Name | Description |
| ---  | ---         |
| `pid` | The PID of the process that owns the listening endpoint |

