package build

type RegistryConfig struct {
	URL       string `json:"url"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	IsDefault bool   `json:"isDefault"`
}

func (r RegistryConfig) ImageRef(project, app, tag string) string {
	return r.URL + "/" + project + "/" + app + ":" + tag
}
