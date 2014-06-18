package cluster

import (
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"net"
	"os"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/dotcloud/docker/daemon/networkdriver/ipallocator"
	"github.com/dotcloud/docker/pkg/iptables"
	"github.com/dotcloud/docker/pkg/netlink"
)

const bridgeName = "flynnbr0"

var bridgeAddr, bridgeNet, _ = net.ParseCIDR("192.168.52.1/24")
var bridge *net.Interface

func initNetworking(natIface string) error {
	if _, err := net.InterfaceByName(bridgeName); err != nil {
		// bridge doesn't exist, create it
		if err := createBridge(); err != nil {
			return err
		}
	}
	bridge, _ = net.InterfaceByName(bridgeName)

	if err := ioutil.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1\n"), 0644); err != nil {
		return err
	}

	if err := setupIPTables(natIface); err != nil {
		return err
	}

	return nil
}

func createBridge() error {
	if err := netlink.CreateBridge(bridgeName, true); err != nil {
		return err
	}
	iface, err := net.InterfaceByName(bridgeName)
	if err != nil {
		return err
	}
	if err := netlink.NetworkLinkAddIp(iface, bridgeAddr, bridgeNet); err != nil {
		return err
	}
	return netlink.NetworkLinkUp(iface)
}

func setupIPTables(natIface string) error {
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
}

func (t *Tap) Close() error {
	if err := deleteTap(t.Name); err != nil {
		return err
	}
	if t.LocalIP != nil {
		ipallocator.ReleaseIP(bridgeNet, t.LocalIP)
	}
	if t.RemoteIP != nil {
		ipallocator.ReleaseIP(bridgeNet, t.RemoteIP)
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
		"Gateway": bridgeAddr.String(),
	})
}

type TapManager struct {
	next uint64
}

func (t *TapManager) NewTap(uid, gid int) (*Tap, error) {
	id := atomic.AddUint64(&t.next, 1) - 1
	tap := &Tap{Name: fmt.Sprintf("flynntap%d", id)}

	if err := createTap(tap.Name, uid, gid); err != nil {
		return nil, err
	}

	var err error
	tap.LocalIP, err = ipallocator.RequestIP(bridgeNet, nil)
	if err != nil {
		tap.Close()
		return nil, err
	}

	tap.RemoteIP, err = ipallocator.RequestIP(bridgeNet, nil)
	if err != nil {
		tap.Close()
		return nil, err
	}

	iface, err := net.InterfaceByName(tap.Name)
	if err != nil {
		tap.Close()
		return nil, err
	}
	if err := netlink.NetworkLinkAddIp(iface, *tap.LocalIP, bridgeNet); err != nil {
		tap.Close()
		return nil, err
	}
	if err := netlink.NetworkLinkUp(iface); err != nil {
		tap.Close()
		return nil, err
	}
	if err := netlink.AddToBridge(iface, bridge); err != nil {
		tap.Close()
		return nil, err
	}

	return tap, nil
}
