package deploy

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type ComposeFile struct {
	Version  string                       `yaml:"version"`
	Services map[string]ComposeServiceSpec `yaml:"services"`
	Volumes  map[string]ComposeVolumeDef  `yaml:"volumes"`
	Networks map[string]ComposeNetworkDef `yaml:"networks"`
}

type ComposeServiceSpec struct {
	Image           string             `yaml:"image"`
	Build           interface{}        `yaml:"build"`
	Ports           []string           `yaml:"ports"`
	Volumes         []string           `yaml:"volumes"`
	Environment     interface{}        `yaml:"environment"`
	Labels          interface{}        `yaml:"labels"`
	DependsOn       interface{}        `yaml:"depends_on"`
	Command         interface{}        `yaml:"command"`
	Entrypoint      interface{}        `yaml:"entrypoint"`
	Restart         string             `yaml:"restart"`
	Deploy          *ComposeDeploy     `yaml:"deploy"`
	Healthcheck     *ComposeHealthcheck `yaml:"healthcheck"`
	CapAdd          []string           `yaml:"cap_add"`
	CapDrop         []string           `yaml:"cap_drop"`
	User            string             `yaml:"user"`
	WorkingDir      string             `yaml:"working_dir"`
	Hostname        string             `yaml:"hostname"`
	Privileged      bool               `yaml:"privileged"`
	Networks        interface{}        `yaml:"networks"`
	StopGracePeriod string             `yaml:"stop_grace_period"`
}

type ComposeHealthcheck struct {
	Test        interface{} `yaml:"test"`
	Interval    string      `yaml:"interval"`
	Timeout     string      `yaml:"timeout"`
	Retries     int         `yaml:"retries"`
	StartPeriod string      `yaml:"start_period"`
}

type ComposeDeploy struct {
	Replicas      int                    `yaml:"replicas"`
	Placement     *ComposePlacement      `yaml:"placement"`
	Resources     *ComposeResources      `yaml:"resources"`
	RestartPolicy *ComposeRestartPolicy  `yaml:"restart_policy"`
	UpdateConfig  *ComposeUpdateConfig   `yaml:"update_config"`
	Labels        interface{}            `yaml:"labels"`
	Mode          string                 `yaml:"mode"`
}

type ComposePlacement struct {
	Constraints []string `yaml:"constraints"`
}

type ComposeResources struct {
	Limits       *ComposeResourceSpec `yaml:"limits"`
	Reservations *ComposeResourceSpec `yaml:"reservations"`
}

type ComposeResourceSpec struct {
	CPUs   string `yaml:"cpus"`
	Memory string `yaml:"memory"`
}

type ComposeRestartPolicy struct {
	Condition   string `yaml:"condition"`
	Delay       string `yaml:"delay"`
	MaxAttempts int    `yaml:"max_attempts"`
	Window      string `yaml:"window"`
}

type ComposeUpdateConfig struct {
	Parallelism   int    `yaml:"parallelism"`
	Delay         string `yaml:"delay"`
	FailureAction string `yaml:"failure_action"`
	Order         string `yaml:"order"`
}

type ComposeVolumeDef struct {
	Driver     string            `yaml:"driver"`
	DriverOpts map[string]string `yaml:"driver_opts"`
	External   bool              `yaml:"external"`
}

type ComposeNetworkDef struct {
	External bool   `yaml:"external"`
	Driver   string `yaml:"driver"`
}

type ParsedService struct {
	Name          string
	ShortName     string
	Image         string
	Ports         []PortMapping
	Volumes       []VolumeMapping
	Environment   map[string]string
	Labels        map[string]string
	Replicas      int
	Constraints   []string
	Command       []string
	Entrypoint    []string
	Healthcheck   *ComposeHealthcheck
	CapAdd        []string
	CapDrop       []string
	User          string
	WorkingDir    string
	Hostname      string
	Resources     *ComposeResources
	RestartPolicy *ComposeRestartPolicy
	UpdateConfig  *ComposeUpdateConfig
	DeployLabels  map[string]string
	ServiceMode   string // "replicated" or "global"
	Networks      []string
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

func ValidateCompose(content string) error {
	cf, err := ParseCompose(content)
	if err != nil {
		return err
	}
	if len(cf.Services) == 0 {
		return fmt.Errorf("compose file has no services")
	}
	for name, svc := range cf.Services {
		if strings.TrimSpace(svc.Image) == "" && svc.Build == nil {
			return fmt.Errorf("service %q must define image or build", name)
		}
		for _, p := range svc.Ports {
			pm := parsePort(p)
			if pm.Target == 0 {
				return fmt.Errorf("service %q has invalid port mapping %q", name, p)
			}
		}
	}
	return nil
}

func ExtractServices(cf *ComposeFile, stackName string) ([]ParsedService, error) {
	var services []ParsedService

	for name, svc := range cf.Services {
		ps := ParsedService{
			Name:        fmt.Sprintf("%s_%s", stackName, name),
			ShortName:   name,
			Image:       svc.Image,
			Environment: parseEnvironment(svc.Environment),
			Labels:      parseLabels(svc.Labels),
			Replicas:    1,
			Command:     parseCommand(svc.Command),
			Entrypoint:  parseCommand(svc.Entrypoint),
			Healthcheck: svc.Healthcheck,
			CapAdd:      svc.CapAdd,
			CapDrop:     svc.CapDrop,
			User:        svc.User,
			WorkingDir:  svc.WorkingDir,
			Hostname:    svc.Hostname,
			ServiceMode: "replicated",
		}

		ps.Networks = parseNetworks(svc.Networks)

		if svc.Deploy != nil {
			if svc.Deploy.Replicas > 0 {
				ps.Replicas = svc.Deploy.Replicas
			}
			if svc.Deploy.Placement != nil {
				ps.Constraints = svc.Deploy.Placement.Constraints
			}
			ps.Resources = svc.Deploy.Resources
			ps.RestartPolicy = svc.Deploy.RestartPolicy
			ps.UpdateConfig = svc.Deploy.UpdateConfig
			ps.DeployLabels = parseLabels(svc.Deploy.Labels)
			if svc.Deploy.Mode == "global" {
				ps.ServiceMode = "global"
			}
		}

		// Default restart policy from compose `restart:` field if deploy didn't set one
		if ps.RestartPolicy == nil && svc.Restart != "" {
			condition := "any"
			switch svc.Restart {
			case "no":
				condition = "none"
			case "on-failure":
				condition = "on-failure"
			case "always", "unless-stopped":
				condition = "any"
			}
			ps.RestartPolicy = &ComposeRestartPolicy{Condition: condition}
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

func parseCommand(cmd interface{}) []string {
	if cmd == nil {
		return nil
	}
	switch v := cmd.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{"sh", "-c", v}
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			result = append(result, fmt.Sprintf("%v", item))
		}
		return result
	}
	return nil
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
	if strings.Contains(portStr, "/udp") {
		pm.Protocol = "udp"
	}
	// Strip protocol suffix for numeric parsing
	numPart := strings.Split(portStr, "/")[0]
	parts := strings.Split(numPart, ":")
	switch len(parts) {
	case 3:
		// host_ip:published:target (e.g. "127.0.0.1:8080:80")
		pm.Published, _ = strconv.Atoi(parts[1])
		pm.Target, _ = strconv.Atoi(parts[2])
	case 2:
		pm.Published, _ = strconv.Atoi(parts[0])
		pm.Target, _ = strconv.Atoi(parts[1])
	default:
		pm.Target, _ = strconv.Atoi(parts[0])
		pm.Published = pm.Target
	}
	return pm
}

func parseVolume(volStr string) VolumeMapping {
	vm := VolumeMapping{}
	parts := strings.Split(volStr, ":")
	switch len(parts) {
	case 1:
		vm.Target = parts[0]
		// Source left empty to indicate an anonymous volume
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

// ParseMemory converts Docker memory strings (e.g. "512M", "1G") to bytes.
func ParseMemory(mem string) int64 {
	if mem == "" {
		return 0
	}
	mem = strings.TrimSpace(mem)
	multiplier := int64(1)
	upper := strings.ToUpper(mem)
	if strings.HasSuffix(upper, "G") || strings.HasSuffix(upper, "GB") {
		multiplier = 1024 * 1024 * 1024
		mem = strings.TrimRight(upper, "GB")
	} else if strings.HasSuffix(upper, "M") || strings.HasSuffix(upper, "MB") {
		multiplier = 1024 * 1024
		mem = strings.TrimRight(upper, "MB")
	} else if strings.HasSuffix(upper, "K") || strings.HasSuffix(upper, "KB") {
		multiplier = 1024
		mem = strings.TrimRight(upper, "KB")
	}
	mem = strings.TrimSpace(mem)
	val, err := strconv.ParseFloat(mem, 64)
	if err != nil {
		return 0
	}
	return int64(val * float64(multiplier))
}

// ParseCPUs converts a CPU string (e.g. "0.5", "2") to NanoCPUs.
func ParseCPUs(cpus string) int64 {
	if cpus == "" {
		return 0
	}
	val, err := strconv.ParseFloat(strings.TrimSpace(cpus), 64)
	if err != nil {
		return 0
	}
	return int64(val * 1e9)
}

// ParseDuration converts Docker duration strings (e.g. "5s", "30s", "1m") to time.Duration.
func ParseDuration(s string) time.Duration {
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0
	}
	return d
}

func parseNetworks(nets interface{}) []string {
	if nets == nil {
		return nil
	}
	switch v := nets.(type) {
	case map[string]interface{}:
		result := make([]string, 0, len(v))
		for name := range v {
			result = append(result, name)
		}
		return result
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			result = append(result, fmt.Sprintf("%v", item))
		}
		return result
	}
	return nil
}

// ParseHealthcheckTest converts compose healthcheck test to []string for the Docker API.
func ParseHealthcheckTest(test interface{}) []string {
	if test == nil {
		return nil
	}
	switch v := test.(type) {
	case string:
		return []string{"CMD-SHELL", v}
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			result = append(result, fmt.Sprintf("%v", item))
		}
		return result
	}
	return nil
}
