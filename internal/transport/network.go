package transport

import (
	"fmt"
	"net"
	"net/netip"
	"strings"

	"github.com/pion/ice/v4"
)

type InterfaceAddress struct {
	Interface string
	IP        net.IP
	Up        bool
}

var sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")

func overlayInterface(name string) bool {
	name = strings.ToLower(name)
	for _, prefix := range []string{"tailscale", "zerotier", "zt", "wg", "tun", "tap", "utun", "nebula", "headscale"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
func DefaultAddress(address InterfaceAddress) bool {
	ip, ok := netip.AddrFromSlice(address.IP)
	if !ok {
		return false
	}
	ip = ip.Unmap()
	return address.Up && (ip.IsLoopback() || ip.IsPrivate() || sharedAddressSpace.Contains(ip) || overlayInterface(address.Interface) && ip.IsGlobalUnicast() && !ip.IsLinkLocalUnicast())
}
func Interfaces() ([]InterfaceAddress, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var addresses []InterfaceAddress
	for _, iface := range interfaces {
		entries, err := iface.Addrs()
		if err != nil {
			return nil, fmt.Errorf("interface %s: %w", iface.Name, err)
		}
		for _, entry := range entries {
			if ip, _, err := net.ParseCIDR(entry.String()); err == nil {
				addresses = append(addresses, InterfaceAddress{iface.Name, ip, iface.Flags&net.FlagUp != 0})
			}
		}
	}
	return addresses, nil
}
func SelectAddresses(addresses []InterfaceAddress, interfaceNames, ipNames string) ([]net.IP, error) {
	names := map[string]bool{}
	wanted := map[string]bool{}
	if interfaceNames != "" {
		for _, name := range strings.Split(interfaceNames, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				return nil, fmt.Errorf("empty interface name")
			}
			names[name] = false
		}
	}
	if ipNames != "" {
		for _, value := range strings.Split(ipNames, ",") {
			ip := net.ParseIP(strings.TrimSpace(value))
			if ip == nil {
				return nil, fmt.Errorf("invalid bind IP %q", value)
			}
			wanted[ip.String()] = false
		}
	}
	seen := map[string]bool{}
	var selected []net.IP
	for _, address := range addresses {
		if !address.Up || address.IP.IsUnspecified() || address.IP.IsMulticast() || address.IP.IsLinkLocalUnicast() || (!address.IP.IsGlobalUnicast() && !address.IP.IsLoopback()) {
			continue
		}
		if len(names) > 0 {
			if _, ok := names[address.Interface]; !ok {
				continue
			}
		}
		if len(wanted) > 0 {
			if _, ok := wanted[address.IP.String()]; !ok {
				continue
			}
		}
		if len(names) == 0 && len(wanted) == 0 && !DefaultAddress(address) {
			continue
		}
		if !seen[address.IP.String()] {
			selected = append(selected, address.IP)
			seen[address.IP.String()] = true
		}
		if len(names) > 0 {
			names[address.Interface] = true
		}
		if len(wanted) > 0 {
			wanted[address.IP.String()] = true
		}
	}
	for name, found := range names {
		if !found {
			return nil, fmt.Errorf("interface %q has no matching active unicast address", name)
		}
	}
	for ip, found := range wanted {
		if !found {
			return nil, fmt.Errorf("bind IP %s is not assigned to a selected active interface", ip)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no usable network addresses; use --list-interfaces and select --interfaces or --bind-ips")
	}
	return selected, nil
}
func LocalIPs() []net.IP {
	addresses, _ := Interfaces()
	ips, _ := SelectAddresses(addresses, "", "")
	return ips
}

// NewUDPMux binds a separate UDP socket per selected IPv4/IPv6 address.
func NewUDPMux(ips []net.IP, port int) (ice.UDPMux, error) {
	if len(ips) == 0 {
		return nil, fmt.Errorf("no selected WebRTC addresses")
	}
	var muxes []ice.UDPMux
	for _, ip := range ips {
		network := "udp6"
		if ip.To4() != nil {
			network = "udp4"
		}
		conn, err := net.ListenUDP(network, &net.UDPAddr{IP: ip, Port: port})
		if err != nil {
			for _, mux := range muxes {
				_ = mux.Close()
			}
			return nil, fmt.Errorf("WebRTC listen on %s: %w", ip, err)
		}
		if port == 0 {
			port = conn.LocalAddr().(*net.UDPAddr).Port
		}
		muxes = append(muxes, ice.NewUDPMuxDefault(ice.UDPMuxParams{UDPConn: conn}))
	}
	return ice.NewMultiUDPMuxDefault(muxes...), nil
}

// ListenTCP uses the same selected addresses as WebRTC when the host is empty.
// An explicit listen host overrides the HTTP binding only.
func ListenTCP(address string, ips []net.IP) ([]net.Listener, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if host != "" {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return nil, err
		}
		return []net.Listener{listener}, nil
	}
	var listeners []net.Listener
	for _, ip := range ips {
		listener, err := net.Listen("tcp", net.JoinHostPort(ip.String(), port))
		if err != nil {
			for _, open := range listeners {
				_ = open.Close()
			}
			return nil, fmt.Errorf("listen on %s: %w", ip, err)
		}
		listeners = append(listeners, listener)
		if port == "0" {
			_, port, _ = net.SplitHostPort(listener.Addr().String())
		}
	}
	if len(listeners) == 0 {
		return nil, fmt.Errorf("no selected listen addresses")
	}
	return listeners, nil
}
