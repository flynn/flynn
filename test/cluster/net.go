package cluster

import (
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"net"
	"os"
	"syscall"
	"unsafe"

	"github.com/docker/libnetwork/ipallocator"
	"github.com/flynn/flynn/pkg/iptables"
	"github.com/flynn/flynn/pkg/random"
	"github.com/vishvananda/netlink"
)

type Bridge struct {
	name   string
	iface  *netlink.Bridge
	ipAddr net.IP
	ipNet  *net.IPNet
	alloc  *ipallocator.IPAllocator
}

func (b *Bridge) IP() string {
	return b.ipAddr.String()
}

func createBridge(name, network, natIface string) (*Bridge, error) {
	la := netlink.NewLinkAttrs()
	la.Name = name
	br := netlink.Link(&netlink.Bridge{LinkAttrs: la})

	ipAddr, ipNet, err := net.ParseCIDR(network)
	if err != nil {
		return nil, err
	}
	if err := netlink.LinkAdd(br); err != nil {
		return nil, err
	}

	iface, err := netlink.LinkByName(name)
	if err != nil {
		return nil, err
	}

	// We need to explicitly assign the MAC address to avoid it changing to a lower value
	// See: https://github.com/flynn/flynn/issues/223
	mac := random.Bytes(5)
	if err := netlink.LinkSetHardwareAddr(iface, append([]byte{0xfe}, mac...)); err != nil {
		return nil, err
	}
	if netlink.AddrAdd(iface, &netlink.Addr{IPNet: &net.IPNet{IP: ipAddr, Mask: ipNet.Mask}}); err != nil {
		return nil, err
	}
	if err := netlink.LinkSetUp(iface); err != nil {
		return nil, err
	}
	if err := ioutil.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1\n"), 0644); err != nil {
		return nil, err
	}
	if err := setupIPTables(name, natIface); err != nil {
		return nil, err
	}

	bridge := &Bridge{
		name:   name,
		iface:  iface.(*netlink.Bridge),
		ipAddr: ipAddr,
		ipNet:  ipNet,
		alloc:  ipallocator.New(),
	}
	bridge.alloc.RequestIP(ipNet, ipAddr)
	return bridge, nil
}

func deleteBridge(bridge *Bridge) error {
	if err := netlink.LinkSetDown(bridge.iface); err != nil {
		return err
	}
	if err := netlink.LinkDel(bridge.iface); err != nil {
		return err
	}
	cleanupIPTables(bridge.name)
	return nil
}

func cleanupIPTables(bridgeName string) {
	// Delete the forwarding rule. The postrouting rule does not need deletion
	// as there is usually only one per box and it doesn't change.
	iptables.Raw("-D", "FORWARD", "-i", bridgeName, "-j", "ACCEPT")
}

func setupIPTables(bridgeName, natIface string) error {
	nat := []string{"POSTROUTING", "-t", "nat", "-o", natIface, "-j", "MASQUERADE"}
	if !iptables.Exists(nat...) {
		if output, err := iptables.Raw(append([]string{"-I"}, nat...)...); err != nil {
			return fmt.Errorf("unable to enable network bridge NAT: %s", err)
		} else if len(output) != 0 {
			return fmt.Errorf("unknown error creating bridge NAT rule: %s", output)
		}
	}

	forward := []string{"FORWARD", "-i", bridgeName, "-j", "ACCEPT"}
	if !iptables.Exists(forward...) {
		if output, err := iptables.Raw(append([]string{"-I"}, forward...)...); err != nil {
			return fmt.Errorf("unable to enable forwarding: %s", err)
		} else if len(output) != 0 {
			return fmt.Errorf("unknown error enabling forwarding: %s", output)
		}
	}

	return nil
}

const (
	IFNAMSIZ      = 16
	IFF_TAP       = 0x0002
	IFF_NO_PI     = 0x1000
	TUNSETIFF     = 0x400454ca
	TUNSETPERSIST = 0x400454cb
	TUNSETOWNER   = 0x400454cc
	TUNSETGROUP   = 0x400454ce
)

type ifreq struct {
	name  [IFNAMSIZ]byte
	flags uint16
}

func ioctl(f *os.File, req int, data uintptr) syscall.Errno {
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(req), data)
	return err
}

func ioctlTap(name string) (*os.File, error) {
	req := ifreq{flags: IFF_NO_PI | IFF_TAP}
	copy(req.name[:IFNAMSIZ-1], name)

	f, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		f.Close()
		return nil, err
	}
	if err := ioctl(f, TUNSETIFF, uintptr(unsafe.Pointer(&req))); err != 0 {
		f.Close()
		return nil, err
	}

	return f, nil
}

func createTap(name string, uid, gid int) error {
	f, err := ioctlTap(name)
	if err != nil {
		return err
	}
	defer f.Close()

	if uid > 0 {
		if err := ioctl(f, TUNSETOWNER, uintptr(uid)); err != 0 {
			return err
		}
	}
	if gid > 0 {
		if err := ioctl(f, TUNSETGROUP, uintptr(gid)); err != 0 {
			return err
		}
	}
	if err := ioctl(f, TUNSETPERSIST, 1); err != 0 {
		return err
	}

	return nil
}

func deleteTap(name string) error {
	f, err := ioctlTap(name)
	if err != nil {
		return err
	}
	defer f.Close()
	if errno := ioctl(f, TUNSETPERSIST, 0); errno != 0 {
		return errno
	}
	return nil
}

type Tap struct {
	Name   string
	IP     net.IP
	bridge *Bridge
}

func (t *Tap) Close() error {
	if err := deleteTap(t.Name); err != nil {
		return err
	}
	if t.IP != nil {
		t.bridge.alloc.ReleaseIP(t.bridge.ipNet, t.IP)
	}
	return nil
}

var ifaceConfig = template.Must(template.New("eth0").Parse(`
auto eth0
iface eth0 inet static
  address {{.Address}}
  gateway {{.Gateway}}
  netmask 255.255.255.0
  dns-nameservers 8.8.8.8 8.8.4.4
`[1:]))

func (t *Tap) WriteInterfaceConfig(f io.Writer) error {
	return ifaceConfig.Execute(f, map[string]string{
		"Address": t.IP.String(),
		"Gateway": t.bridge.IP(),
	})
}

type TapManager struct {
	bridge *Bridge
}

func (t *TapManager) NewTap(uid, gid int) (*Tap, error) {
	tap := &Tap{Name: "flynntap." + random.String(5), bridge: t.bridge}

	if err := createTap(tap.Name, uid, gid); err != nil {
		return nil, err
	}

	var err error
	tap.IP, err = t.bridge.alloc.RequestIP(t.bridge.ipNet, nil)
	if err != nil {
		tap.Close()
		return nil, err
	}

	iface, err := netlink.LinkByName(tap.Name)
	if err != nil {
		tap.Close()
		return nil, err
	}
	if err := netlink.LinkSetUp(iface); err != nil {
		tap.Close()
		return nil, err
	}
	if err := netlink.LinkSetMaster(iface, t.bridge.iface); err != nil {
		tap.Close()
		return nil, err
	}

	return tap, nil
}
