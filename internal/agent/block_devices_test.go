package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConvertDevice(t *testing.T) {
	mp := "/mnt/data"
	path := "/dev/sda"
	model := "WDC WD10EZEX"
	serial := "ABC123"
	tran := "sata"
	fstype := "ext4"

	dev := lsblkDevice{
		Name:       "sda",
		Size:       float64(1000000000000),
		Type:       "disk",
		MountPoint: &mp,
		FSType:     &fstype,
		Model:      &model,
		Serial:     &serial,
		Rota:       true,
		Tran:       &tran,
		Path:       &path,
	}

	bd := convertDevice(dev)
	assert.Equal(t, "sda", bd.Name)
	assert.Equal(t, "/dev/sda", bd.Path)
	assert.Equal(t, uint64(1000000000000), bd.Size)
	assert.Equal(t, "disk", bd.Type)
	assert.Equal(t, "/mnt/data", bd.MountPoint)
	assert.Equal(t, "ext4", bd.FSType)
	assert.Equal(t, "WDC WD10EZEX", bd.Model)
	assert.Equal(t, "ABC123", bd.Serial)
	assert.True(t, bd.Rotational)
	assert.Equal(t, "sata", bd.Transport)
	assert.False(t, bd.Available) // mounted disk is not available
}

func TestIsAvailableForOSD_EmptyDisk(t *testing.T) {
	dev := lsblkDevice{
		Name: "sdb",
		Size: float64(500000000000),
		Type: "disk",
	}
	assert.True(t, isAvailableForOSD(dev))
}

func TestIsAvailableForOSD_MountedDisk(t *testing.T) {
	mp := "/"
	dev := lsblkDevice{
		Name:       "sda",
		Size:       float64(500000000000),
		Type:       "disk",
		MountPoint: &mp,
	}
	assert.False(t, isAvailableForOSD(dev))
}

func TestIsAvailableForOSD_USBDisk(t *testing.T) {
	tran := "usb"
	dev := lsblkDevice{
		Name: "sdc",
		Size: float64(500000000000),
		Type: "disk",
		Tran: &tran,
	}
	assert.False(t, isAvailableForOSD(dev))
}

func TestIsAvailableForOSD_TooSmall(t *testing.T) {
	dev := lsblkDevice{
		Name: "sdd",
		Size: float64(500000000), // ~500MB, less than 1GB
		Type: "disk",
	}
	assert.False(t, isAvailableForOSD(dev))
}

func TestIsAvailableForOSD_Partition(t *testing.T) {
	dev := lsblkDevice{
		Name: "sda1",
		Size: float64(500000000000),
		Type: "part",
	}
	assert.False(t, isAvailableForOSD(dev))
}

func TestIsAvailableForOSD_DiskWithMountedChild(t *testing.T) {
	childMP := "/boot"
	dev := lsblkDevice{
		Name: "sda",
		Size: float64(500000000000),
		Type: "disk",
		Children: []lsblkDevice{
			{Name: "sda1", Size: float64(500000000), Type: "part", MountPoint: &childMP},
		},
	}
	assert.False(t, isAvailableForOSD(dev))
}

func TestParseSize(t *testing.T) {
	assert.Equal(t, uint64(1000), parseSize(float64(1000)))
	assert.Equal(t, uint64(2000), parseSize("2000"))
	assert.Equal(t, uint64(0), parseSize(nil))
}

func TestParseBool(t *testing.T) {
	assert.True(t, parseBool(true))
	assert.True(t, parseBool(float64(1)))
	assert.True(t, parseBool("1"))
	assert.True(t, parseBool("true"))
	assert.False(t, parseBool(false))
	assert.False(t, parseBool(float64(0)))
	assert.False(t, parseBool("0"))
}

func TestDerefStr(t *testing.T) {
	s := "hello"
	assert.Equal(t, "hello", derefStr(&s))
	assert.Equal(t, "", derefStr(nil))
}
