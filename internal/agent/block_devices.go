package agent

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
)

type lsblkOutput struct {
	BlockDevices []lsblkDevice `json:"blockdevices"`
}

type lsblkDevice struct {
	Name       string        `json:"name"`
	Size       interface{}   `json:"size"`
	Type       string        `json:"type"`
	MountPoint *string       `json:"mountpoint"`
	FSType     *string       `json:"fstype"`
	Model      *string       `json:"model"`
	Serial     *string       `json:"serial"`
	Rota       interface{}   `json:"rota"`
	Tran       *string       `json:"tran"`
	Path       *string       `json:"path"`
	Children   []lsblkDevice `json:"children,omitempty"`
}

func collectBlockDevices(report *NodeMetricsReport) {
	out, err := exec.Command(
		"lsblk", "--json", "--bytes",
		"--output", "NAME,SIZE,TYPE,MOUNTPOINT,FSTYPE,MODEL,SERIAL,ROTA,TRAN,PATH",
	).Output()
	if err != nil {
		return
	}

	var parsed lsblkOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return
	}

	for _, dev := range parsed.BlockDevices {
		bd := convertDevice(dev)
		report.BlockDevices = append(report.BlockDevices, bd)
	}
}

func convertDevice(dev lsblkDevice) BlockDevice {
	bd := BlockDevice{
		Name:       dev.Name,
		Path:       derefStr(dev.Path),
		Size:       parseSize(dev.Size),
		Type:       dev.Type,
		MountPoint: derefStr(dev.MountPoint),
		FSType:     derefStr(dev.FSType),
		Model:      strings.TrimSpace(derefStr(dev.Model)),
		Serial:     strings.TrimSpace(derefStr(dev.Serial)),
		Rotational: parseBool(dev.Rota),
		Transport:  derefStr(dev.Tran),
	}
	if bd.Path == "" {
		bd.Path = "/dev/" + bd.Name
	}
	bd.Available = isAvailableForOSD(dev)
	return bd
}

func isAvailableForOSD(dev lsblkDevice) bool {
	if dev.Type != "disk" {
		return false
	}
	if derefStr(dev.MountPoint) != "" || derefStr(dev.FSType) != "" {
		return false
	}
	if len(dev.Children) > 0 {
		for _, child := range dev.Children {
			if derefStr(child.MountPoint) != "" || derefStr(child.FSType) != "" {
				return false
			}
		}
	}
	if derefStr(dev.Tran) == "usb" {
		return false
	}
	if parseSize(dev.Size) < 1<<30 {
		return false
	}
	return true
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func parseSize(v interface{}) uint64 {
	switch val := v.(type) {
	case float64:
		return uint64(val)
	case string:
		n, _ := strconv.ParseUint(val, 10, 64)
		return n
	case json.Number:
		n, _ := val.Int64()
		return uint64(n)
	}
	return 0
}

func parseBool(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case float64:
		return val != 0
	case string:
		return val == "1" || val == "true"
	}
	return false
}
