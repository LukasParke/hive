package catalog

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Template struct {
	Kind           string            `yaml:"kind" json:"kind"`
	Name           string            `yaml:"name" json:"name"`
	Description    string            `yaml:"description" json:"description"`
	Category       string            `yaml:"category" json:"category"`
	Icon           string            `yaml:"icon" json:"icon"`
	Image          string            `yaml:"image" json:"image"`
	Ports          []string          `yaml:"ports" json:"ports"`
	Volumes        []string          `yaml:"volumes" json:"volumes"`
	Env            map[string]string `yaml:"env" json:"env"`
	Domain         string            `yaml:"domain" json:"domain"`
	Replicas       int               `yaml:"replicas" json:"replicas"`
	DependsOn      []Dependency      `yaml:"depends_on" json:"depends_on"`
	HomepageLabels map[string]string `yaml:"homepage_labels" json:"homepage_labels"`
	TraefikLabels  map[string]string `yaml:"traefik_labels" json:"traefik_labels"`
	NASVolumes     []NASVolume       `yaml:"nas_volumes" json:"nas_volumes"`
	IsStack        bool              `yaml:"stack" json:"stack"`
	Services       []StackService    `yaml:"services" json:"services"`
}

type Dependency struct {
	Type    string `yaml:"type" json:"type"`
	Version string `yaml:"version" json:"version"`
}

type NASVolume struct {
	Name          string `yaml:"name" json:"name"`
	SuggestedPath string `yaml:"suggested_path" json:"suggested_path"`
	Description   string `yaml:"description" json:"description"`
}

type StackService struct {
	Name    string            `yaml:"name" json:"name"`
	Image   string            `yaml:"image" json:"image"`
	Ports   []string          `yaml:"ports" json:"ports"`
	Env     map[string]string `yaml:"env" json:"env"`
	Volumes []string          `yaml:"volumes" json:"volumes"`
}

type Catalog struct {
	templates []Template
}

func LoadFromDir(dir string) (*Catalog, error) {
	c := &Catalog{}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, fmt.Errorf("read templates dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read template %s: %w", entry.Name(), err)
		}

		var tmpl Template
		if err := yaml.Unmarshal(data, &tmpl); err != nil {
			return nil, fmt.Errorf("parse template %s: %w", entry.Name(), err)
		}

		if tmpl.Replicas == 0 {
			tmpl.Replicas = 1
		}
		c.templates = append(c.templates, tmpl)
	}

	return c, nil
}

func (c *Catalog) List() []Template {
	return c.templates
}

func (c *Catalog) Get(name string) (*Template, error) {
	for _, t := range c.templates {
		if t.Name == name {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("template %q not found", name)
}
