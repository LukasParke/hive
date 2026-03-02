package agent

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func collectDisk(report *NodeMetricsReport) {
	mountsFile := filepath.Join(hostProc, "mounts")
	data, err := os.ReadFile(mountsFile)
	if err != nil {
		return
	}

	seen := make(map[string]bool)

	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		device := fields[0]
		mountPoint := fields[1]
		fsType := fields[2]

		if !isPhysicalFS(fsType) {
			continue
		}
		if seen[mountPoint] {
			continue
		}
		seen[mountPoint] = true

		var stat syscall.Statfs_t
		if err := syscall.Statfs(mountPoint, &stat); err != nil {
			hostMountPoint := "/host" + mountPoint
			if err := syscall.Statfs(hostMountPoint, &stat); err != nil {
				continue
			}
		}

		total := stat.Blocks * uint64(stat.Bsize)
		free := stat.Bfree * uint64(stat.Bsize)

		report.Disks = append(report.Disks, DiskMetrics{
			MountPoint: mountPoint,
			Device:     device,
			FSType:     fsType,
			Total:      total,
			Used:       total - free,
		})
	}

	collectDiskIO(report)
}

func isPhysicalFS(fsType string) bool {
	switch fsType {
	case "ext4", "ext3", "ext2", "xfs", "btrfs", "zfs", "ntfs", "vfat", "fat32",
		"f2fs", "reiserfs", "jfs", "bcachefs", "nfs", "nfs4", "cifs", "ceph":
		return true
	}
	return false
}

func collectDiskIO(report *NodeMetricsReport) {
	data, err := os.ReadFile(filepath.Join(hostProc, "diskstats"))
	if err != nil {
		return
	}

	deviceStats := make(map[string][2]uint64)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 14 {
			continue
		}
		devName := fields[2]
		readSectors, _ := strconv.ParseUint(fields[5], 10, 64)
		writeSectors, _ := strconv.ParseUint(fields[9], 10, 64)
		deviceStats[devName] = [2]uint64{readSectors * 512, writeSectors * 512}
	}

	for i, disk := range report.Disks {
		baseDev := filepath.Base(disk.Device)
		if stats, ok := deviceStats[baseDev]; ok {
			report.Disks[i].ReadBytes = stats[0]
			report.Disks[i].WriteBytes = stats[1]
		}
	}
}
