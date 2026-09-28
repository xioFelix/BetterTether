package tun

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// utun configuration constants for macOS
const (
	AF_SYSTEM            = 32
	SYSPROTO_CONTROL     = 2
	AF_SYS_CONTROL       = 2
	UTUN_CONTROL_NAME    = "com.apple.net.utun_control"
	droidTetherServiceID = "BetterTether"
)

// utunInterface implements Interface for Darwin using AF_SYSTEM.
type utunInterface struct {
	f                *os.File
	name             string
	address          string // local IP assigned via Configure
	mu               sync.Mutex
	closed           bool
	routesAdded      [2]bool
	scopedRouteAdded bool
	scopedGateway    string
	dnsSet           bool
}

func (i *utunInterface) Read(p []byte) (n int, err error) {
	return i.f.Read(p)
}

func (i *utunInterface) Write(p []byte) (n int, err error) {
	return i.f.Write(p)
}

func (i *utunInterface) Close() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return nil
	}
	i.closed = true
	if i.scopedRouteAdded && scopedRouteBelongsTo(i.name, i.scopedGateway) {
		_ = exec.Command("route", "delete", "-net", "-ifscope", i.name, "default", i.scopedGateway).Run()
	}
	for n, prefix := range [...]string{"0.0.0.0/1", "128.0.0.0/1"} {
		if i.routesAdded[n] && routeBelongsTo(prefix, i.name) {
			_ = exec.Command("route", "delete", "-net", prefix).Run()
		}
	}
	if i.dnsSet {
		var script strings.Builder
		script.WriteString("open\n")
		// Other network managers may have replaced either global key. Never
		// overwrite their state with an empty dictionary on disconnect.
		if dynamicStoreContains("State:/Network/Global/IPv4", "PrimaryService : "+droidTetherServiceID) {
			script.WriteString("remove State:/Network/Global/IPv4\n")
		}
		if dynamicStoreContains("State:/Network/Global/DNS", "__CONFIGURATION_ID__ : Supplemental: "+droidTetherServiceID) {
			script.WriteString("remove State:/Network/Global/DNS\n")
		}
		for _, kind := range [...]string{"DNS", "IPv4", "Interface"} {
			fmt.Fprintf(&script, "remove State:/Network/Service/%s/%s\n", droidTetherServiceID, kind)
		}
		script.WriteString("quit\n")
		cmd := exec.Command("scutil")
		cmd.Stdin = strings.NewReader(script.String())
		_ = cmd.Run()
	}
	return i.f.Close()
}

func routeBelongsTo(prefix, interfaceName string) bool {
	out, err := exec.Command("route", "-n", "get", "-net", prefix).CombinedOutput()
	return err == nil && routeOutputBelongsTo(string(out), prefix, interfaceName)
}

func routeOutputBelongsTo(output, prefix, interfaceName string) bool {
	wantDestination := "default"
	if prefix == "128.0.0.0/1" {
		wantDestination = "128.0.0.0"
	}
	var destination, mask, netif string
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		switch key {
		case "destination":
			destination = strings.TrimSpace(value)
		case "mask":
			mask = strings.TrimSpace(value)
		case "interface":
			netif = strings.TrimSpace(value)
		}
	}
	return destination == wantDestination && mask == "128.0.0.0" && netif == interfaceName
}

func scopedRouteBelongsTo(interfaceName, gateway string) bool {
	out, err := exec.Command("route", "-n", "get", "-ifscope", interfaceName, "1.1.1.1").CombinedOutput()
	return err == nil && scopedRouteOutputBelongsTo(string(out), interfaceName, gateway)
}

func scopedRouteOutputBelongsTo(output, interfaceName, gateway string) bool {
	return strings.Contains(output, "destination: default\n") &&
		strings.Contains(output, "mask: default\n") &&
		strings.Contains(output, "gateway: "+gateway+"\n") &&
		strings.Contains(output, "interface: "+interfaceName+"\n") &&
		strings.Contains(output, "IFSCOPE")
}

func dynamicStoreContains(key, value string) bool {
	cmd := exec.Command("scutil")
	cmd.Stdin = strings.NewReader("show " + key + "\nquit\n")
	out, err := cmd.CombinedOutput()
	return err == nil && strings.Contains(string(out), value)
}
func (i *utunInterface) Name() string {
	return i.name
}

// Configure sets the IP addresses and MTU for the utun interface using the 'ifconfig' command.
func (i *utunInterface) Configure(localIP, remoteIP, mtu string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return fmt.Errorf("utun: cannot configure after close")
	}
	i.address = localIP
	// Formula: ifconfig <name> <local> <remote> mtu <val> up
	cmd := exec.Command("ifconfig", i.name, localIP, remoteIP, "mtu", mtu, "up")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ifconfig failed: %w (output: %s)", err, string(out))
	}
	return nil
}

// SetDefaultRoute adds "more specific" default routes (0.0.0.0/1 and 128.0.0.0/1)
// to override the existing default route without deleting it.
func (i *utunInterface) SetDefaultRoute(gateway string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return fmt.Errorf("utun: cannot add routes after close")
	}
	if i.routesAdded[0] && i.routesAdded[1] {
		return nil
	}
	// 0.0.0.0/1
	cmd1 := exec.Command("route", "add", "-net", "0.0.0.0/1", "-interface", i.name)
	if out, err := cmd1.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add 0/1 route: %w (output: %s)", err, string(out))
	}
	i.routesAdded[0] = true

	// 128.0.0.0/1
	cmd2 := exec.Command("route", "add", "-net", "128.0.0.0/1", "-interface", i.name)
	if out, err := cmd2.CombinedOutput(); err != nil {
		if routeBelongsTo("0.0.0.0/1", i.name) {
			_ = exec.Command("route", "delete", "-net", "0.0.0.0/1").Run()
		}
		i.routesAdded[0] = false
		return fmt.Errorf("failed to add 128/1 route: %w (output: %s)", err, string(out))
	}
	i.routesAdded[1] = true

	return nil
}

// SetScopedDefaultRoute makes the phone usable by an explicitly bound Surge
// policy without competing with Surge's system-wide VIF default route.
func (i *utunInterface) SetScopedDefaultRoute(gateway string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return fmt.Errorf("utun: cannot add scoped route after close")
	}
	// A previous daemon may have been killed before it could remove its /1
	// routes. This utun belongs to our current session, so clear those stale
	// routes before letting Surge's VIF take the unscoped default route.
	for _, prefix := range [...]string{"0.0.0.0/1", "128.0.0.0/1"} {
		if routeBelongsTo(prefix, i.name) {
			if out, err := exec.Command("route", "delete", "-net", prefix).CombinedOutput(); err != nil {
				return fmt.Errorf("failed to remove stale %s route: %w (output: %s)", prefix, err, string(out))
			}
		}
	}
	if i.scopedRouteAdded {
		return nil
	}
	if scopedRouteBelongsTo(i.name, gateway) {
		// Adopt a matching scoped route left by an interrupted session so it
		// is removed when this utun closes.
		i.scopedRouteAdded = true
		i.scopedGateway = gateway
		return nil
	}
	cmd := exec.Command("route", "add", "-net", "-ifscope", i.name, "default", gateway)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add scoped default route: %w (output: %s)", err, string(out))
	}
	i.scopedRouteAdded = true
	i.scopedGateway = gateway
	return nil
}

// SetDNS sets the system DNS to the phone gateway (or other provided servers)
// using 'scutil' on macOS. It also registers a proper network service with
// PrimaryService and PrimaryInterface so that macOS SCNetworkReachability
// reports the system as online — fixing Safari, App Store, and system updates.
func (i *utunInterface) SetDNS(dnsServers []string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return fmt.Errorf("utun: cannot set DNS after close")
	}
	if len(dnsServers) == 0 {
		return nil
	}

	gateway := dnsServers[len(dnsServers)-1]

	cmd := exec.Command("scutil")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	go func() {
		defer stdin.Close()
		fmt.Fprintln(stdin, "open")

		// ── Service-specific DNS ────────────────────────────────────────
		fmt.Fprintln(stdin, "d.init")
		fmt.Fprint(stdin, "d.add ServerAddresses *")
		for _, s := range dnsServers {
			fmt.Fprintf(stdin, " %s", s)
		}
		fmt.Fprintln(stdin)
		fmt.Fprintln(stdin, "d.add SupplementalMatchDomains * \"\"")
		fmt.Fprintln(stdin, "d.add SupplementalMatchOrders * 10")
		fmt.Fprintf(stdin, "set State:/Network/Service/%s/DNS\n", droidTetherServiceID)

		// ── Service-specific IPv4 ────────────────────────────────────────
		fmt.Fprintln(stdin, "d.init")
		if i.address != "" {
			fmt.Fprintf(stdin, "d.add Addresses * %s\n", i.address)
		}
		fmt.Fprintf(stdin, "d.add InterfaceName %s\n", i.name)
		fmt.Fprintf(stdin, "d.add Router %s\n", gateway)
		fmt.Fprintf(stdin, "set State:/Network/Service/%s/IPv4\n", droidTetherServiceID)

		// ── Service-specific Interface metadata ─────────────────────────
		fmt.Fprintln(stdin, "d.init")
		fmt.Fprintf(stdin, "d.add DeviceName %s\n", i.name)
		fmt.Fprintln(stdin, "d.add Type Other")
		fmt.Fprintf(stdin, "set State:/Network/Service/%s/Interface\n", droidTetherServiceID)

		// ── Global IPv4 – mark this service as primary ──────────────────
		// This is what SCNetworkReachability reads to determine if the
		// system is online. Without PrimaryService, Safari/App Store fail.
		fmt.Fprintln(stdin, "d.init")
		fmt.Fprintf(stdin, "d.add PrimaryInterface %s\n", i.name)
		fmt.Fprintf(stdin, "d.add PrimaryService %s\n", droidTetherServiceID)
		fmt.Fprintf(stdin, "d.add Router %s\n", gateway)
		fmt.Fprintln(stdin, "set State:/Network/Global/IPv4")

		// ── Global DNS ──────────────────────────────────────────────────
		fmt.Fprintln(stdin, "d.init")
		fmt.Fprint(stdin, "d.add ServerAddresses *")
		for _, s := range dnsServers {
			fmt.Fprintf(stdin, " %s", s)
		}
		fmt.Fprintln(stdin)
		fmt.Fprintln(stdin, "d.add SupplementalMatchDomains * \"\"")
		fmt.Fprintln(stdin, "d.add SupplementalMatchOrders * 10")
		fmt.Fprintln(stdin, "set State:/Network/Global/DNS")

		fmt.Fprintln(stdin, "quit")
	}()

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("scutil network setup failed: %w (output: %s)", err, string(out))
	}
	i.dnsSet = true

	return nil
}

// OpenUTUN creates a new utun interface on macOS.
// If index is 0, the system chooses the first available (utun0, utun1, etc.).
func OpenUTUN(index int) (Interface, error) {
	fd, err := unix.Socket(AF_SYSTEM, unix.SOCK_DGRAM, SYSPROTO_CONTROL)
	if err != nil {
		return nil, fmt.Errorf("utun: failed to open system socket: %w", err)
	}

	// 1. Find the control ID for "com.apple.net.utun_control"
	info := struct {
		ctl_id   uint32
		ctl_name [96]byte
	}{}
	copy(info.ctl_name[:], UTUN_CONTROL_NAME)

	// CTLIOCGINFO
	err = ioctl(fd, 0xc0644e03, unsafe.Pointer(&info))
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("utun: failed to get utun control info: %w", err)
	}

	// 2. Connect to the utun control
	sc := struct {
		sc_len      uint8
		sc_family   uint8
		ss_sysaddr  uint16
		sc_id       uint32
		sc_unit     uint32
		sc_reserved [5]uint32
	}{
		sc_len:     32,
		sc_family:  AF_SYSTEM,
		ss_sysaddr: AF_SYS_CONTROL,
		sc_id:      info.ctl_id,
		sc_unit:    uint32(index), // 0 = automatic
	}

	err = connect(fd, unsafe.Pointer(&sc), 32)
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("utun: failed to connect to utun control: %w", err)
	}

	// 3. Get the interface name (e.g., utun3)
	nameBuf := make([]byte, 64)
	nameLen := uint32(len(nameBuf))
	// UTUN_OPT_IFNAME (Option 2)
	err = getsockopt(fd, SYSPROTO_CONTROL, 2, unsafe.Pointer(&nameBuf[0]), &nameLen)
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("utun: failed to get interface name: %w", err)
	}

	ifname := string(nameBuf[:nameLen-1]) // trim null byte
	return &utunInterface{
		f:    os.NewFile(uintptr(fd), ifname),
		name: ifname,
	}, nil
}

// Wrapper for unix.Ioctl
func ioctl(fd int, request uintptr, argp unsafe.Pointer) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), request, uintptr(argp))
	if errno != 0 {
		return errno
	}
	return nil
}

// Wrapper for unix.Connect
func connect(fd int, addr unsafe.Pointer, len uint32) error {
	_, _, errno := unix.Syscall(unix.SYS_CONNECT, uintptr(fd), uintptr(addr), uintptr(len))
	if errno != 0 {
		return errno
	}
	return nil
}

// Wrapper for unix.Getsockopt
func getsockopt(fd int, level, name int, val unsafe.Pointer, len *uint32) error {
	_, _, errno := unix.Syscall6(unix.SYS_GETSOCKOPT, uintptr(fd), uintptr(level), uintptr(name), uintptr(val), uintptr(unsafe.Pointer(len)), 0)
	if errno != 0 {
		return errno
	}
	return nil
}
