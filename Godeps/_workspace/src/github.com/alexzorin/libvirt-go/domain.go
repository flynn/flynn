package libvirt

/*
#cgo LDFLAGS: -lvirt -ldl
#include <libvirt/libvirt.h>
#include <libvirt/virterror.h>
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"reflect"
	"strings"
	"unsafe"
)

type VirDomain struct {
	ptr C.virDomainPtr
}

type VirDomainInfo struct {
	ptr C.virDomainInfo
}

type VirTypedParameter struct {
	Name  string
	Value interface{}
}

type VirTypedParameters []VirTypedParameter

func (dest *VirTypedParameters) loadFromCPtr(params C.virTypedParameterPtr, nParams int) {
	// reset slice
	*dest = VirTypedParameters{}

	// transform that C array to a go slice
	hdr := reflect.SliceHeader{
		Data: uintptr(unsafe.Pointer(params)),
		Len:  int(nParams),
		Cap:  int(nParams),
	}
	rawParams := *(*[]C.struct__virTypedParameter)(unsafe.Pointer(&hdr))

	// there is probably a more elegant way to deal with that union
	for _, rawParam := range rawParams {
		name := C.GoStringN(&rawParam.field[0], C.VIR_TYPED_PARAM_FIELD_LENGTH)
		if nbIdx := strings.Index(name, "\x00"); nbIdx != -1 {
			name = name[:nbIdx]
		}
		switch rawParam._type {
		case C.VIR_TYPED_PARAM_INT:
			*dest = append(*dest, VirTypedParameter{name, int(*(*C.int)(unsafe.Pointer(&rawParam.value[0])))})
		case C.VIR_TYPED_PARAM_UINT:
			*dest = append(*dest, VirTypedParameter{name, uint32(*(*C.uint)(unsafe.Pointer(&rawParam.value[0])))})
		case C.VIR_TYPED_PARAM_LLONG:
			*dest = append(*dest, VirTypedParameter{name, int64(*(*C.longlong)(unsafe.Pointer(&rawParam.value[0])))})
		case C.VIR_TYPED_PARAM_ULLONG:
			*dest = append(*dest, VirTypedParameter{name, uint64(*(*C.ulonglong)(unsafe.Pointer(&rawParam.value[0])))})
		case C.VIR_TYPED_PARAM_DOUBLE:
			*dest = append(*dest, VirTypedParameter{name, float64(*(*C.double)(unsafe.Pointer(&rawParam.value[0])))})
		case C.VIR_TYPED_PARAM_BOOLEAN:
			if int(*(*C.char)(unsafe.Pointer(&rawParam.value[0]))) == 1 {
				*dest = append(*dest, VirTypedParameter{name, true})
			} else {
				*dest = append(*dest, VirTypedParameter{name, false})
			}
		case C.VIR_TYPED_PARAM_STRING:
			*dest = append(*dest, VirTypedParameter{name, C.GoString((*C.char)(unsafe.Pointer(*(*uintptr)(unsafe.Pointer(&rawParam.value[0])))))})
		}
	}
}

func (d *VirDomain) Free() error {
	if result := C.virDomainFree(d.ptr); result != 0 {
		return errors.New(GetLastError())
	}
	return nil
}

func (d *VirDomain) Create() error {
	result := C.virDomainCreate(d.ptr)
	if result == -1 {
		return errors.New(GetLastError())
	}
	return nil
}

func (d *VirDomain) Destroy() error {
	result := C.virDomainDestroy(d.ptr)
	if result == -1 {
		return errors.New(GetLastError())
	}
	return nil
}

func (d *VirDomain) Shutdown() error {
	result := C.virDomainShutdown(d.ptr)
	if result == -1 {
		return errors.New(GetLastError())
	}
	return nil
}

func (d *VirDomain) Reboot(flags uint) error {
	result := C.virDomainReboot(d.ptr, C.uint(flags))
	if result == -1 {
		return errors.New(GetLastError())
	}
	return nil
}

func (d *VirDomain) IsActive() (bool, error) {
	result := C.virDomainIsActive(d.ptr)
	if result == -1 {
		return false, errors.New(GetLastError())
	}
	if result == 1 {
		return true, nil
	}
	return false, nil
}

func (d *VirDomain) SetAutostart(autostart bool) error {
	var cAutostart C.int
	switch autostart {
	case true:
		cAutostart = 1
	default:
		cAutostart = 0
	}
	result := C.virDomainSetAutostart(d.ptr, cAutostart)
	if result == -1 {
		return errors.New(GetLastError())
	}
	return nil
}

func (d *VirDomain) GetAutostart() (bool, error) {
	var out C.int
	result := C.virDomainGetAutostart(d.ptr, (*C.int)(unsafe.Pointer(&out)))
	if result == -1 {
		return false, errors.New(GetLastError())
	}
	switch out {
	case 1:
		return true, nil
	default:
		return false, nil
	}
}

func (d *VirDomain) GetName() (string, error) {
	name := C.virDomainGetName(d.ptr)
	if name == nil {
		return "", errors.New(GetLastError())
	}
	return C.GoString(name), nil
}

func (d *VirDomain) GetState() ([]int, error) {
	var cState C.int
	var cReason C.int
	result := C.virDomainGetState(d.ptr,
		(*C.int)(unsafe.Pointer(&cState)),
		(*C.int)(unsafe.Pointer(&cReason)),
		0)
	if int(result) == -1 {
		return []int{}, errors.New(GetLastError())
	}
	return []int{int(cState), int(cReason)}, nil
}

func (d *VirDomain) GetUUID() ([]byte, error) {
	var cUuid [C.VIR_UUID_BUFLEN](byte)
	cuidPtr := unsafe.Pointer(&cUuid)
	result := C.virDomainGetUUID(d.ptr, (*C.uchar)(cuidPtr))
	if result != 0 {
		return []byte{}, errors.New(GetLastError())
	}
	return C.GoBytes(cuidPtr, C.VIR_UUID_BUFLEN), nil
}

func (d *VirDomain) GetUUIDString() (string, error) {
	var cUuid [C.VIR_UUID_STRING_BUFLEN](C.char)
	cuidPtr := unsafe.Pointer(&cUuid)
	result := C.virDomainGetUUIDString(d.ptr, (*C.char)(cuidPtr))
	if result != 0 {
		return "", errors.New(GetLastError())
	}
	return C.GoString((*C.char)(cuidPtr)), nil
}

func (d *VirDomain) GetInfo() (VirDomainInfo, error) {
	di := VirDomainInfo{}
	var ptr C.virDomainInfo
	result := C.virDomainGetInfo(d.ptr, (*C.virDomainInfo)(unsafe.Pointer(&ptr)))
	if result == -1 {
		return di, errors.New(GetLastError())
	}
	di.ptr = ptr
	return di, nil
}

func (d *VirDomain) GetXMLDesc(flags uint32) (string, error) {
	result := C.virDomainGetXMLDesc(d.ptr, C.uint(flags))
	if result == nil {
		return "", errors.New(GetLastError())
	}
	xml := C.GoString(result)
	C.free(unsafe.Pointer(result))
	return xml, nil
}

func (i *VirDomainInfo) GetState() uint8 {
	return uint8(i.ptr.state)
}

func (i *VirDomainInfo) GetMaxMem() uint64 {
	return uint64(i.ptr.maxMem)
}

func (i *VirDomainInfo) GetMemory() uint64 {
	return uint64(i.ptr.memory)
}

func (i *VirDomainInfo) GetNrVirtCpu() uint16 {
	return uint16(i.ptr.nrVirtCpu)
}

func (i *VirDomainInfo) GetCpuTime() uint64 {
	return uint64(i.ptr.cpuTime)
}

func (d *VirDomain) GetCPUStats(params *VirTypedParameters, nParams int, startCpu int, nCpus uint32, flags uint32) (int, error) {
	var cParams C.virTypedParameterPtr
	var cParamsLen int

	cParamsLen = int(nCpus) * nParams

	if params != nil && cParamsLen > 0 {
		cParams = (C.virTypedParameterPtr)(C.calloc(C.size_t(cParamsLen), C.size_t(unsafe.Sizeof(C.struct__virTypedParameter{}))))
		defer C.virTypedParamsFree(cParams, C.int(cParamsLen))
	} else {
		cParamsLen = 0
		cParams = nil
	}

	result := int(C.virDomainGetCPUStats(d.ptr, (C.virTypedParameterPtr)(cParams), C.uint(nParams), C.int(startCpu), C.uint(nCpus), C.uint(flags)))
	if result == -1 {
		return result, errors.New(GetLastError())
	}

	if cParamsLen > 0 {
		params.loadFromCPtr(cParams, cParamsLen)
	}

	return result, nil
}

// Warning: No test written for this function
func (d *VirDomain) GetInterfaceParameters(device string, params *VirTypedParameters, nParams *int, flags uint32) (int, error) {
	var cParams C.virTypedParameterPtr

	if params != nil && *nParams > 0 {
		cParams = (C.virTypedParameterPtr)(C.calloc(C.size_t(*nParams), C.size_t(unsafe.Sizeof(C.struct__virTypedParameter{}))))
		defer C.virTypedParamsFree(cParams, C.int(*nParams))
	} else {
		cParams = nil
	}

	result := int(C.virDomainGetInterfaceParameters(d.ptr, C.CString(device), (C.virTypedParameterPtr)(cParams), (*C.int)(unsafe.Pointer(nParams)), C.uint(flags)))
	if result == -1 {
		return result, errors.New(GetLastError())
	}

	if params != nil && *nParams > 0 {
		params.loadFromCPtr(cParams, *nParams)
	}

	return result, nil
}

func (d *VirDomain) GetMetadata(tipus int, uri string, flags uint32) (string, error) {
	var cUri *C.char
	if uri != "" {
		cUri = C.CString(uri)
		defer C.free(unsafe.Pointer(cUri))
	}

	result := C.virDomainGetMetadata(d.ptr, C.int(tipus), cUri, C.uint(flags))
	if result == nil {
		return "", errors.New(GetLastError())

	}
	defer C.free(unsafe.Pointer(result))
	return C.GoString(result), nil
}

func (d *VirDomain) SetMetadata(metaDataType int, metaDataCont, uriKey, uri string, flags uint32) error {
	var cMetaDataCont *C.char
	var cUriKey *C.char
	var cUri *C.char

	cMetaDataCont = C.CString(metaDataCont)
	defer C.free(unsafe.Pointer(cMetaDataCont))

	if metaDataType == VIR_DOMAIN_METADATA_ELEMENT {
		cUriKey = C.CString(uriKey)
		defer C.free(unsafe.Pointer(cUriKey))
		cUri = C.CString(uri)
		defer C.free(unsafe.Pointer(cUri))
	}
	result := C.virDomainSetMetadata(d.ptr, C.int(metaDataType), cMetaDataCont, cUriKey, cUri, C.uint(flags))
	if result == -1 {
		return errors.New(GetLastError())
	}
	return nil
}

func (d *VirDomain) Undefine() error {
	result := C.virDomainUndefine(d.ptr)
	if result == -1 {
		return errors.New(GetLastError())
	}
	return nil
}

func (d *VirDomain) SetMaxMemory(memory uint) error {
	result := C.virDomainSetMaxMemory(d.ptr, C.ulong(memory))
	if result == -1 {
		return errors.New(GetLastError())
	}
	return nil
}

func (d *VirDomain) SetMemory(memory uint64) error {
	result := C.virDomainSetMemory(d.ptr, C.ulong(memory))
	if result == -1 {
		return errors.New(GetLastError())
	}
	return nil
}

func (d *VirDomain) SetMemoryFlags(memory uint64, flags uint32) error {
	result := C.virDomainSetMemoryFlags(d.ptr, C.ulong(memory), C.uint(flags))
	if result == -1 {
		return errors.New(GetLastError())
	}
	return nil
}

func (d *VirDomain) SetMemoryStatsPeriod(period int, flags uint) error {
	result := C.virDomainSetMemoryStatsPeriod(d.ptr, C.int(period), C.uint(flags))
	if result == -1 {
		return errors.New(GetLastError())
	}
	return nil
}

func (d *VirDomain) SetVcpus(vcpu uint) error {
	result := C.virDomainSetVcpus(d.ptr, C.uint(vcpu))
	if result == -1 {
		return errors.New(GetLastError())
	}
	return nil
}

func (d *VirDomain) SetVcpusFlags(vcpu uint, flags uint) error {
	result := C.virDomainSetVcpusFlags(d.ptr, C.uint(vcpu), C.uint(flags))
	if result == -1 {
		return errors.New(GetLastError())
	}
	return nil
}

func (d *VirDomain) Suspend() error {
	result := C.virDomainSuspend(d.ptr)
	if result == -1 {
		return errors.New(GetLastError())
	}
	return nil
}

func (d *VirDomain) Resume() error {
	result := C.virDomainResume(d.ptr)
	if result == -1 {
		return errors.New(GetLastError())
	}
	return nil
}

func (d *VirDomain) AbortJob() error {
	result := C.virDomainAbortJob(d.ptr)
	if result == -1 {
		return errors.New(GetLastError())
	}
	return nil
}

func (d *VirDomain) DestroyFlags(flags uint) error {
	result := C.virDomainDestroyFlags(d.ptr, C.uint(flags))
	if result == -1 {
		return errors.New(GetLastError())
	}
	return nil
}

func (d *VirDomain) ShutdownFlags(flags uint) error {
	result := C.virDomainShutdownFlags(d.ptr, C.uint(flags))
	if result == -1 {
		return errors.New(GetLastError())
	}
	return nil
}

func (d *VirDomain) AttachDevice(xml string) error {
	cXml := C.CString(xml)
	defer C.free(unsafe.Pointer(cXml))
	result := C.virDomainAttachDevice(d.ptr, cXml)
	if result == -1 {
		return errors.New(GetLastError())
	}
	return nil
}

func (d *VirDomain) AttachDeviceFlags(xml string, flags uint) error {
	cXml := C.CString(xml)
	defer C.free(unsafe.Pointer(cXml))
	result := C.virDomainAttachDeviceFlags(d.ptr, cXml, C.uint(flags))
	if result == -1 {
		return errors.New(GetLastError())
	}
	return nil
}

func (d *VirDomain) DetachDevice(xml string) error {
	cXml := C.CString(xml)
	defer C.free(unsafe.Pointer(cXml))
	result := C.virDomainDetachDevice(d.ptr, cXml)
	if result == -1 {
		return errors.New(GetLastError())
	}
	return nil
}

func (d *VirDomain) DetachDeviceFlags(xml string, flags uint) error {
	cXml := C.CString(xml)
	defer C.free(unsafe.Pointer(cXml))
	result := C.virDomainDetachDeviceFlags(d.ptr, cXml, C.uint(flags))
	if result == -1 {
		return errors.New(GetLastError())
	}
	return nil
}
