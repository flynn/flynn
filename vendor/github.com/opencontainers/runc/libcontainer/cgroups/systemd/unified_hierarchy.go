// +build linux,!static_build

package systemd

import (
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	systemdDbus "github.com/coreos/go-systemd/dbus"
	"github.com/opencontainers/runc/libcontainer/cgroups"
	"github.com/opencontainers/runc/libcontainer/cgroups/fs"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/sirupsen/logrus"
)

type UnifiedManager struct {
	mu      sync.Mutex
	Cgroups *configs.Cgroup
	Paths   map[string]string
}

var unifiedSubsystems = subsystemSet{
	&fs.CpusetGroupV2{},
	&fs.FreezerGroupV2{},
	&fs.CpuGroupV2{},
	&fs.MemoryGroupV2{},
	&fs.IOGroupV2{},
	&fs.PidsGroupV2{},
}

func (m *UnifiedManager) Apply(pid int) error {
	var (
		c          = m.Cgroups
		unitName   = getUnitName(c)
		slice      = "system.slice"
		properties []systemdDbus.Property
	)

	if c.Paths != nil {
		paths := make(map[string]string)
		for name, path := range c.Paths {
			_, err := getSubsystemPath(m.Cgroups, name)
			if err != nil {
				// Don't fail if a cgroup hierarchy was not found, just skip this subsystem
				if cgroups.IsNotFound(err) {
					continue
				}
				return err
			}
			paths[name] = path
		}
		m.Paths = paths
		return cgroups.EnterPid(m.Paths, pid)
	}

	if c.Parent != "" {
		slice = c.Parent
	}

	properties = append(properties, systemdDbus.PropDescription("libcontainer container "+c.Name))

	// if we create a slice, the parent is defined via a Wants=
	if strings.HasSuffix(unitName, ".slice") {
		properties = append(properties, systemdDbus.PropWants(slice))
	} else {
		// otherwise, we use Slice=
		properties = append(properties, systemdDbus.PropSlice(slice))
	}

	// only add pid if its valid, -1 is used w/ general slice creation.
	if pid != -1 {
		properties = append(properties, newProp("PIDs", []uint32{uint32(pid)}))
	}

	// Check if we can delegate. This is only supported on systemd versions 218 and above.
	if !strings.HasSuffix(unitName, ".slice") {
		// Assume scopes always support delegation.
		properties = append(properties, newProp("Delegate", true))
	}

	// Always enable accounting, this gets us the same behaviour as the fs implementation,
	// plus the kernel has some problems with joining the memory cgroup at a later time.
	properties = append(properties,
		newProp("MemoryAccounting", true),
		newProp("CPUAccounting", true),
		newProp("BlockIOAccounting", true))

	// Assume DefaultDependencies= will always work (the check for it was previously broken.)
	properties = append(properties,
		newProp("DefaultDependencies", false))

	if c.Resources.Memory != 0 {
		properties = append(properties,
			newProp("MemoryLimit", uint64(c.Resources.Memory)))
	}

	if c.Resources.CpuShares != 0 {
		properties = append(properties,
			newProp("CPUShares", c.Resources.CpuShares))
	}

	// cpu.cfs_quota_us and cpu.cfs_period_us are controlled by systemd.
	if c.Resources.CpuQuota != 0 && c.Resources.CpuPeriod != 0 {
		// corresponds to USEC_INFINITY in systemd
		// if USEC_INFINITY is provided, CPUQuota is left unbound by systemd
		// always setting a property value ensures we can apply a quota and remove it later
		cpuQuotaPerSecUSec := uint64(math.MaxUint64)
		if c.Resources.CpuQuota > 0 {
			// systemd converts CPUQuotaPerSecUSec (microseconds per CPU second) to CPUQuota
			// (integer percentage of CPU) internally.  This means that if a fractional percent of
			// CPU is indicated by Resources.CpuQuota, we need to round up to the nearest
			// 10ms (1% of a second) such that child cgroups can set the cpu.cfs_quota_us they expect.
			cpuQuotaPerSecUSec = uint64(c.Resources.CpuQuota*1000000) / c.Resources.CpuPeriod
			if cpuQuotaPerSecUSec%10000 != 0 {
				cpuQuotaPerSecUSec = ((cpuQuotaPerSecUSec / 10000) + 1) * 10000
			}
		}
		properties = append(properties,
			newProp("CPUQuotaPerSecUSec", cpuQuotaPerSecUSec))
	}

	if c.Resources.BlkioWeight != 0 {
		properties = append(properties,
			newProp("BlockIOWeight", uint64(c.Resources.BlkioWeight)))
	}

	if c.Resources.PidsLimit > 0 {
		properties = append(properties,
			newProp("TasksAccounting", true),
			newProp("TasksMax", uint64(c.Resources.PidsLimit)))
	}

	// We have to set kernel memory here, as we can't change it once
	// processes have been attached to the cgroup.
	if c.Resources.KernelMemory != 0 {
		if err := setKernelMemory(c); err != nil {
			return err
		}
	}

	statusChan := make(chan string, 1)
	if _, err := theConn.StartTransientUnit(unitName, "replace", properties, statusChan); err == nil {
		select {
		case <-statusChan:
		case <-time.After(time.Second):
			logrus.Warnf("Timed out while waiting for StartTransientUnit(%s) completion signal from dbus. Continuing...", unitName)
		}
	} else if !isUnitExists(err) {
		return err
	}

	if err := joinCgroupsV2(c, pid); err != nil {
		return err
	}

	paths := make(map[string]string)
	for _, s := range unifiedSubsystems {
		subsystemPath, err := getSubsystemPath(m.Cgroups, s.Name())
		if err != nil {
			// Don't fail if a cgroup hierarchy was not found, just skip this subsystem
			if cgroups.IsNotFound(err) {
				continue
			}
			return err
		}
		paths[s.Name()] = subsystemPath
	}
	m.Paths = paths
	return nil
}

func (m *UnifiedManager) Destroy() error {
	if m.Cgroups.Paths != nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	theConn.StopUnit(getUnitName(m.Cgroups), "replace", nil)
	if err := cgroups.RemovePaths(m.Paths); err != nil {
		return err
	}
	m.Paths = make(map[string]string)
	return nil
}

func (m *UnifiedManager) GetPaths() map[string]string {
	m.mu.Lock()
	paths := m.Paths
	m.mu.Unlock()
	return paths
}

func createCgroupsv2Path(path string) (Err error) {
	content, err := ioutil.ReadFile("/sys/fs/cgroup/cgroup.controllers")
	if err != nil {
		return err
	}
	if !filepath.HasPrefix(path, "/sys/fs/cgroup") {
		return fmt.Errorf("invalid cgroup path %s", path)
	}

	res := ""
	for i, c := range strings.Split(strings.TrimSpace(string(content)), " ") {
		if i == 0 {
			res = fmt.Sprintf("+%s", c)
		} else {
			res = res + fmt.Sprintf(" +%s", c)
		}
	}
	resByte := []byte(res)

	current := "/sys/fs"
	elements := strings.Split(path, "/")
	for i, e := range elements[3:] {
		current = filepath.Join(current, e)
		if i > 0 {
			if err := os.Mkdir(current, 0755); err != nil {
				if !os.IsExist(err) {
					return err
				}
			} else {
				// If the directory was created, be sure it is not left around on errors.
				defer func() {
					if Err != nil {
						os.Remove(current)
					}
				}()
			}
		}
		if i < len(elements[3:])-1 {
			if err := ioutil.WriteFile(filepath.Join(current, "cgroup.subtree_control"), resByte, 0755); err != nil {
				return err
			}
		}
	}
	return nil
}

func joinCgroupsV2(c *configs.Cgroup, pid int) error {
	path, err := getSubsystemPath(c, "memory")
	if err != nil {
		return err
	}
	return createCgroupsv2Path(path)
}

func (m *UnifiedManager) Freeze(state configs.FreezerState) error {
	path, err := getSubsystemPath(m.Cgroups, "freezer")
	if err != nil {
		return err
	}
	prevState := m.Cgroups.Resources.Freezer
	m.Cgroups.Resources.Freezer = state
	freezer, err := unifiedSubsystems.Get("freezer")
	if err != nil {
		return err
	}
	err = freezer.Set(path, m.Cgroups)
	if err != nil {
		m.Cgroups.Resources.Freezer = prevState
		return err
	}
	return nil
}

func (m *UnifiedManager) GetPids() ([]int, error) {
	path, err := getSubsystemPath(m.Cgroups, "devices")
	if err != nil {
		return nil, err
	}
	return cgroups.GetPids(path)
}

func (m *UnifiedManager) GetAllPids() ([]int, error) {
	path, err := getSubsystemPath(m.Cgroups, "devices")
	if err != nil {
		return nil, err
	}
	return cgroups.GetAllPids(path)
}

func (m *UnifiedManager) GetStats() (*cgroups.Stats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stats := cgroups.NewStats()
	for name, path := range m.Paths {
		sys, err := unifiedSubsystems.Get(name)
		if err == errSubsystemDoesNotExist || !cgroups.PathExists(path) {
			continue
		}
		if err := sys.GetStats(path, stats); err != nil {
			return nil, err
		}
	}

	return stats, nil
}

func (m *UnifiedManager) Set(container *configs.Config) error {
	// If Paths are set, then we are just joining cgroups paths
	// and there is no need to set any values.
	if m.Cgroups.Paths != nil {
		return nil
	}
	for _, sys := range unifiedSubsystems {
		// Get the subsystem path, but don't error out for not found cgroups.
		path, err := getSubsystemPath(container.Cgroups, sys.Name())
		if err != nil && !cgroups.IsNotFound(err) {
			return err
		}

		if err := sys.Set(path, container.Cgroups); err != nil {
			return err
		}
	}

	if m.Paths["cpu"] != "" {
		if err := fs.CheckCpushares(m.Paths["cpu"], container.Cgroups.Resources.CpuShares); err != nil {
			return err
		}
	}
	return nil
}
