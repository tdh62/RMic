package transport

import (
	"io"
	"net"
	"testing"
)

func TestDefaultInterfacesIncludeTailscaleAndOverlays(t *testing.T) {
	fixtures := []struct {
		name, ip string
		want     bool
	}{
		{"eth0", "192.168.1.20", true}, {"eth1", "10.3.0.8", true},
		{"tailscale0", "100.101.102.103", true}, {"vpn", "100.64.0.2", true},
		{"tailscale0", "fd7a:115c:a1e0::1234", true}, {"wg0", "fd00:1234::1", true},
		{"ztabc", "203.0.113.8", true}, {"eth2", "203.0.113.8", false},
		{"eth0", "fe80::1", false}, {"eth0", "169.254.1.2", false},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name+fixture.ip, func(t *testing.T) {
			address := InterfaceAddress{fixture.name, net.ParseIP(fixture.ip), true}
			if got := DefaultAddress(address); got != fixture.want {
				t.Fatalf("default=%t want=%t", got, fixture.want)
			}
			address.Up = false
			if DefaultAddress(address) {
				t.Fatal("down interface selected")
			}
		})
	}
	addresses := []InterfaceAddress{{"eth0", net.ParseIP("192.168.1.20"), true}, {"tailscale0", net.ParseIP("100.101.102.103"), true}, {"tailscale0", net.ParseIP("fd7a:115c:a1e0::1234"), true}, {"public0", net.ParseIP("203.0.113.8"), true}}
	defaults, err := SelectAddresses(addresses, "", "")
	if err != nil || len(defaults) != 3 {
		t.Fatalf("missing LAN/overlay addresses: %v %v", defaults, err)
	}
	selected, err := SelectAddresses(addresses, "public0", "")
	if err != nil || len(selected) != 1 || selected[0].String() != "203.0.113.8" {
		t.Fatalf("explicit public interface failed: %v %v", selected, err)
	}
	selected, err = SelectAddresses(addresses, "tailscale0", "fd7a:115c:a1e0::1234")
	if err != nil || len(selected) != 1 {
		t.Fatalf("IPv6 selection failed: %v %v", selected, err)
	}
	for _, selection := range [][2]string{{"missing", ""}, {"", "198.51.100.4"}, {"eth0", "100.101.102.103"}, {"", "invalid"}} {
		if _, err = SelectAddresses(addresses, selection[0], selection[1]); err == nil {
			t.Fatalf("invalid selection accepted: %v", selection)
		}
	}
}

func TestHTTPSAndUDPBindAllSelectedAddresses(t *testing.T) {
	ips := []net.IP{net.ParseIP("127.0.0.1")}
	if listener, err := net.Listen("tcp6", "[::1]:0"); err == nil {
		_ = listener.Close()
		ips = append(ips, net.ParseIP("::1"))
	}
	listeners, err := ListenTCP(":0", ips)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()
	mux, err := NewUDPMux(ips, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer mux.Close()
	if len(mux.GetListenAddresses()) != len(ips) {
		t.Fatal("not all selected UDP addresses were bound")
	}
	tcpPort, udpPort := "", ""
	for _, listener := range listeners {
		_, port, _ := net.SplitHostPort(listener.Addr().String())
		if tcpPort != "" && port != tcpPort {
			t.Fatal("different HTTPS ports per interface")
		}
		tcpPort = port
		done := make(chan error, 1)
		go func(listener net.Listener) {
			conn, err := listener.Accept()
			if err == nil {
				_, err = conn.Write([]byte("ok"))
				_ = conn.Close()
			}
			done <- err
		}(listener)
		conn, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		value, err := io.ReadAll(conn)
		_ = conn.Close()
		if err != nil || string(value) != "ok" {
			t.Fatalf("listener unreachable: %s %v", value, err)
		}
		if err = <-done; err != nil {
			t.Fatal(err)
		}
	}
	for _, address := range mux.GetListenAddresses() {
		_, port, _ := net.SplitHostPort(address.String())
		if udpPort != "" && port != udpPort {
			t.Fatal("different UDP ports per interface")
		}
		udpPort = port
	}
	if len(listeners) != len(ips) {
		t.Fatal("not all selected HTTPS addresses were bound")
	}
}
