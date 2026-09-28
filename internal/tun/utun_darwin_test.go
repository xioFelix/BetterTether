package tun

import "testing"

func TestRouteOutputBelongsTo(t *testing.T) {
	const zeroHalf = `   route to: default
destination: default
       mask: 128.0.0.0
  interface: utun4
      flags: <UP,DONE,STATIC,PRCLONING,GLOBAL>
`
	const upperHalf = `   route to: 128.0.0.0
destination: 128.0.0.0
       mask: 128.0.0.0
  interface: utun4
`
	for _, tc := range []struct {
		name, output, prefix, iface string
		want                        bool
	}{
		{"owned lower half", zeroHalf, "0.0.0.0/1", "utun4", true},
		{"owned upper half", upperHalf, "128.0.0.0/1", "utun4", true},
		{"different interface", zeroHalf, "0.0.0.0/1", "utun12", false},
		{"wrong half", upperHalf, "0.0.0.0/1", "utun4", false},
		{"not a half route", "destination: default\nmask: default\ninterface: utun4\n", "0.0.0.0/1", "utun4", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := routeOutputBelongsTo(tc.output, tc.prefix, tc.iface); got != tc.want {
				t.Fatalf("route ownership = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestScopedRouteOutputBelongsTo(t *testing.T) {
	const scoped = `   route to: 1.1.1.1
destination: default
       mask: default
    gateway: 10.174.183.21
  interface: utun4
      flags: <UP,GATEWAY,DONE,STATIC,PRCLONING,IFSCOPE,GLOBAL>
`
	if !scopedRouteOutputBelongsTo(scoped, "utun4", "10.174.183.21") {
		t.Fatal("expected owned scoped route")
	}
	for _, tc := range []struct{ iface, gateway string }{
		{"utun12", "10.174.183.21"},
		{"utun4", "10.174.183.22"},
	} {
		if scopedRouteOutputBelongsTo(scoped, tc.iface, tc.gateway) {
			t.Fatal("must not claim another interface or gateway")
		}
	}
	if scopedRouteOutputBelongsTo("destination: default\nmask: default\ngateway: 10.174.183.21\ninterface: utun4\n", "utun4", "10.174.183.21") {
		t.Fatal("must not claim an unscoped default route")
	}
}
