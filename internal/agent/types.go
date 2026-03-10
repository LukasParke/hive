package agent

type BlockDevice struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Size       uint64 `json:"size"`
	Type       string `json:"type"`
	MountPoint string `json:"mount_point,omitempty"`
	FSType     string `json:"fs_type,omitempty"`
	Model      string `json:"model,omitempty"`
	Serial     string `json:"serial,omitempty"`
	Rotational bool   `json:"rotational"`
	Transport  string `json:"transport,omitempty"`
	Available  bool   `json:"available"`
}
