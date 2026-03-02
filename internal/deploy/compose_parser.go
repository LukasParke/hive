package deploy

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type ComposeFile struct {
	Version  string                        `yaml:"version"`
	Services map[string]ComposeServiceSpec `yaml:"services"`
	Volumes  map[string]ComposeVolumeDef   `yaml:"volumes"`
	Networks map[string]ComposeNetworkDef  `yaml:"networks"`
}

type ComposeServiceSpec struct {
	Image       string            `yaml:"image"`
	Build       interface{}       `yaml:"build"`
	Ports       []string          `yaml:"ports"`
	Volumes     []string          `yaml:"volumes"`
	Environment interface{}       `yaml:"environment"`
	Labels      interface{}       `yaml:"labels"`
	DependsOn   interface{}       `yaml:"depends_on"`
	Command     interface{}       `yaml:"command"`
	Restart     string            `yaml:"restart"`
	Deploy      *ComposeDeploy    `yaml:"deploy"`
}

type ComposeDeploy struct {
	Replicas  int                `yaml:"replicas"`
	Placement *ComposePlacement  `yaml:"placement"`
}

type ComposePlacement struct {
	Constraints []string `yaml:"constraints"`
}

type ComposeVolumeDef struct {
	Driver     string            `yaml:"driver"`
	DriverOpts map[string]string `yaml:"driver_opts"`
	External   bool              `yaml:"external"`
}

type ComposeNetworkDef struct {
	External bool `yaml:"external"`
}

type ParsedService struct {
	Name        string
	Image       string
	Ports       []PortMapping
	Volumes     []VolumeMapping
	Environment map[string]string
	Labels      map[string]string
	Replicas    int
	Constraints []string
}

type PortMapping struct {
	Published int
	Target    int
	Protocol  string
}

type VolumeMapping struct {
	Source   string
	Target  string
	ReadOnly bool
}

func ParseCompose(content string) (*ComposeFile, error) {
	var cf ComposeFile
	if err := yaml.Unmarshal([]byte(content), &cf); err != nil {
		return nil, fmt.Errorf("parse compose: %w", err)
	}
	return &cf, nil
}

func ExtractServices(cf *ComposeFile, stackName string) ([]ParsedService, error) {
	var services []ParsedService

	for name, svc := range cf.Services {
		ps := ParsedService{
			Name:        fmt.Sprintf("%s_%s", stackName, name),
			Image:       svc.Image,
			Environment: parseEnvironment(svc.Environment),
			Labels:      parseLabels(svc.Labels),
			Replicas:    1,
		}

		if svc.Deploy != nil {
			if svc.Deploy.Replicas > 0 {
				ps.Replicas = svc.Deploy.Replicas
			}
			if svc.Deploy.Placement != nil {
				ps.Constraints = svc.Deploy.Placement.Constraints
			}
		}

		for _, p := range svc.Ports {
			pm := parsePort(p)
			ps.Ports = append(ps.Ports, pm)
		}

		for _, v := range svc.Volumes {
			vm := parseVolume(v)
			ps.Volumes = append(ps.Volumes, vm)
		}

		services = append(services, ps)
	}

	return services, nil
}

func parseEnvironment(env interface{}) map[string]string {
	result := make(map[string]string)
	if env == nil {
		return result
	}

	switch v := env.(type) {
	case map[string]interface{}:
		for key, val := range v {
			result[key] = fmt.Sprintf("%v", val)
		}
	case []interface{}:
		for _, item := range v {
			s := fmt.Sprintf("%v", item)
			parts := strings.SplitN(s, "=", 2)
			if len(parts) == 2 {
				result[parts[0]] = parts[1]
			} else {
				result[parts[0]] = ""
			}
		}
	}
	return result
}

func parseLabels(labels interface{}) map[string]string {
	result := make(map[string]string)
	if labels == nil {
		return result
	}

	switch v := labels.(type) {
	case map[string]interface{}:
		for key, val := range v {
			result[key] = fmt.Sprintf("%v", val)
		}
	case []interface{}:
		for _, item := range v {
			s := fmt.Sprintf("%v", item)
			parts := strings.SplitN(s, "=", 2)
			if len(parts) == 2 {
				result[parts[0]] = parts[1]
			}
		}
	}
	return result
}

func parsePort(portStr string) PortMapping {
	pm := PortMapping{Protocol: "tcp"}
	parts := strings.Split(portStr, ":")
	if len(parts) == 2 {
		pm.Published, _ = strconv.Atoi(parts[0])
		pm.Target, _ = strconv.Atoi(strings.Split(parts[1], "/")[0])
	} else {
		pm.Target, _ = strconv.Atoi(strings.Split(parts[0], "/")[0])
		pm.Published = pm.Target
	}
	if strings.Contains(portStr, "/udp") {
		pm.Protocol = "udp"
	}
	return pm
}

func parseVolume(volStr string) VolumeMapping {
	vm := VolumeMapping{}
	parts := strings.Split(volStr, ":")
	switch len(parts) {
	case 1:
		vm.Target = parts[0]
		vm.Source = parts[0]
	case 2:
		vm.Source = parts[0]
		vm.Target = parts[1]
	case 3:
		vm.Source = parts[0]
		vm.Target = parts[1]
		vm.ReadOnly = parts[2] == "ro"
	}
	return vm
}
