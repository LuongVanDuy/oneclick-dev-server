package platform

import "runtime"

type Info struct {
	OS                     string `json:"os"`
	VirtualizationBackend  string `json:"virtualizationBackend"`
	VirtualizationReady    bool   `json:"virtualizationReady"`
	VirtualizationMessage  string `json:"virtualizationMessage"`
}

func Detect() Info {
	switch runtime.GOOS {
	case "windows":
		return Info{
			OS:                    "windows",
			VirtualizationBackend: "hyper-v",
			VirtualizationReady:   false,
			VirtualizationMessage: "Hyper-V adapter is planned; VM provisioning is not enabled in this milestone.",
		}
	case "linux":
		return Info{
			OS:                    "linux",
			VirtualizationBackend: "kvm/libvirt",
			VirtualizationReady:   false,
			VirtualizationMessage: "KVM/libvirt adapter is planned; VM provisioning is not enabled in this milestone.",
		}
	default:
		return Info{
			OS:                    runtime.GOOS,
			VirtualizationBackend: "unsupported",
			VirtualizationReady:   false,
			VirtualizationMessage: "This operating system does not have a virtualization adapter yet.",
		}
	}
}
