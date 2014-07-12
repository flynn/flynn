package libvirt

import "encoding/xml"

type Domain struct {
	XMLName xml.Name `xml:"domain"`
	ID      int      `xml:"id,attr,omitempty"`
	Type    string   `xml:"type,attr"`
	Name    string   `xml:"name,omitempty"`
	UUID    string   `xml:"uuid,omitempty"`
	// TODO: metadata

	OS    OS     `xml:"os"`
	IDMap *IDMap `xml:"idmap,omitempty"`

	Memory UnitInt `xml:"memory"`
	VCPU   int     `xml:"vcpu"`

	OnPoweroff string `xml:"on_poweroff,omitempty"`
	OnReboot   string `xml:"on_reboot,omitempty"`
	OnCrash    string `xml:"on_crash,omitempty"`

	Devices Devices `xml:"devices"`
}

func (d *Domain) XML() []byte {
	data, _ := xml.Marshal(d)
	return data
}

type OS struct {
	Type     OSType   `xml:"type"`
	Init     string   `xml:"init"`
	InitArgs []string `xml:"initarg,omitempty"`
}

type OSType struct {
	Value   string `xml:",chardata"`
	Arch    string `xml:"arch,attr,omitempty"`
	Machine string `xml:"machine,attr,omitempty"`
}

type IDMap struct {
	Uid IDMapping `xml:"uid"`
	Gid IDMapping `xml:"gid"`
}

type IDMapping struct {
	Start  int `xml:"start,attr"`
	Target int `xml:"target,attr"`
	Count  int `xml:"count,attr"`
}

type UnitInt struct {
	Value int    `xml:",chardata"`
	Unit  string `xml:"unit,attr,omitempty"`
}

type Devices struct {
	Emulator    string       `xml:"emulator,omitempty"`
	Filesystems []Filesystem `xml:"filesystem"`
	HostDevs    []HostDev    `xml:"hostdev"`
	Interfaces  []Interface  `xml:"interface"`
	Consoles    []Console    `xml:"console"`
}

type Filesystem struct {
	Type   string    `xml:"type,attr,omitempty"`
	Driver *FSDriver `xml:"driver,omitempty"`
	Source FSRef     `xml:"source"`
	Target FSRef     `xml:"target"`
}

type FSDriver struct {
	Name   string `xml:"name,attr,omitempty"`
	Type   string `xml:"type,attr,omitempty"`
	Format string `xml:"format,attr,omitempty"`
}

type FSRef struct {
	Dir  string `xml:"dir,attr,omitempty"`
	File string `xml:"file,attr,omitempty"`
}

type HostDev struct {
	Mode         string `xml:"mode,attr"`
	Type         string `xml:"type,attr"`
	SrcBlock     string `xml:"source>block,omitempty"`
	SrcChar      string `xml:"source>char,omitempty"`
	SrcInterface string `xml:"source>interface,omitempty"`
}

type Interface struct {
	Type   string        `xml:"type,attr"`
	Source InterfaceSrc  `xml:"source"`
	Target *InterfaceSrc `xml:"target,omitempty"`
}

type InterfaceSrc struct {
	Network string `xml:"network,attr,omitempty"`
	Dev     string `xml:"dev,attr,omitempty"`
	Mode    string `xml:"mode,attr,omitempty"`
}

type Console struct {
	Type string `xml:"type,attr"`
}
