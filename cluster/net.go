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

	"github.com/docker/libcontainer/netlink"
	"github.com/dotcloud/docker/daemon/networkdriver/ipallocator"
	"github.com/flynn/flynn-test/util"
	"github.com/flynn/go-iptables"
)

type Bridge struct {
	name   string
	iface  *net.Interface
	ipAddr net.IP
	ipNet  *net.IPNet
}

func (b *Bridge) IP() string {
	return b.ipAddr.String()
}

func createBridge(name, network, natIface string) (*Bridge, error) {
	ipAddr, ipNet, err := net.ParseCIDR(network)
	if err != nil {
		return nil, err
	}
	if err := netlink.CreateBridge(name, true); err != nil {
		return nil, err
	}
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, err
	}
	if err := netlink.NetworkLinkAddIp(iface, ipAddr, ipNet); err != nil {
		return nil, err
	}
	if err := netlink.NetworkLinkUp(iface); err != nil {
		return nil, err
	}
	if err := ioutil.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1\n"), 0644); err != nil {
		return nil, err
	}
	if err := setupIPTables(name, natIface); err != nil {
		return nil, err
	}
	return &Bridge{name, iface, ipAddr, ipNet}, nil
}

func deleteBridge(bridge *Bridge) error {
	if err := netlink.NetworkLinkDown(bridge.iface); err != nil {
		return err
	}
	if err := netlink.DeleteBridge(bridge.name); err != nil {
		return err
	}
	return nil
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
	Name              string
	LocalIP, RemoteIP *net.IP
	bridge            *Bridge
}

func (t *Tap) Close() error {
	if err := deleteTap(t.Name); err != nil {
		return err
	}
	if t.LocalIP != nil {
		ipallocator.ReleaseIP(t.bridge.ipNet, t.LocalIP)
	}
	if t.RemoteIP != nil {
		ipallocator.ReleaseIP(t.bridge.ipNet, t.RemoteIP)
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
		"Address": t.RemoteIP.String(),
		"Gateway": t.bridge.IP(),
	})
}

type TapManager struct {
	bridge *Bridge
}

func (t *TapManager) NewTap(uid, gid int) (*Tap, error) {
	tap := &Tap{Name: "flynntap." + util.RandomString(5), bridge: t.bridge}

	if err := createTap(tap.Name, uid, gid); err != nil {
		return nil, err
	}

	var err error
	tap.LocalIP, err = ipallocator.RequestIP(t.bridge.ipNet, nil)
	if err != nil {
		tap.Close()
		return nil, err
	}

	tap.RemoteIP, err = ipallocator.RequestIP(t.bridge.ipNet, nil)
	if err != nil {
		tap.Close()
		return nil, err
	}

	iface, err := net.InterfaceByName(tap.Name)
	if err != nil {
		tap.Close()
		return nil, err
	}
	if err := netlink.NetworkLinkAddIp(iface, *tap.LocalIP, t.bridge.ipNet); err != nil {
		tap.Close()
		return nil, err
	}
	if err := netlink.NetworkLinkUp(iface); err != nil {
		tap.Close()
		return nil, err
	}
	if err := netlink.AddToBridge(iface, t.bridge.iface); err != nil {
		tap.Close()
		return nil, err
	}

	return tap, nil
}
