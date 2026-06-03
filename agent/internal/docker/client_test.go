package docker

import (
	"testing"
)

func TestValidateContainerID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"valid short", "abc123", false},
		{"valid sha256", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2", false},
		{"valid with dots", "my.container-1", false},
		{"valid with underscore", "my_container", false},
		{"empty", "", true},
		{"starts with dot", ".invalid", true},
		{"starts with dash", "-invalid", true},
		{"contains space", "has space", true},
		{"contains slash", "has/slash", true},
		{"too long", string(make([]byte, 129)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateContainerID(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateContainerID(%q) error = %v, wantErr %v", tt.id, err, tt.wantErr)
			}
		})
	}
}

func TestValidateShell(t *testing.T) {
	tests := []struct {
		shell   string
		wantErr bool
	}{
		{"/bin/bash", false},
		{"/bin/sh", false},
		{"/bin/zsh", false},
		{"/bin/ash", false},
		{"/bin/fish", true},
		{"/usr/bin/python", true},
		{"bash", true},
		{"", true},
	}
	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			err := ValidateShell(tt.shell)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateShell(%q) error = %v, wantErr %v", tt.shell, err, tt.wantErr)
			}
		})
	}
}

func TestCalculateCPUPercent(t *testing.T) {
	tests := []struct {
		name       string
		preCPU     uint64
		curCPU     uint64
		preSystem  uint64
		curSystem  uint64
		onlineCPUs uint64
		want       float64
	}{
		{
			name:       "50% on 4 CPUs",
			preCPU:     100000,
			curCPU:     200000,
			preSystem:  1000000,
			curSystem:  1800000,
			onlineCPUs: 4,
			want:       50.0,
		},
		{
			name:       "zero system delta",
			preCPU:     100,
			curCPU:     200,
			preSystem:  1000,
			curSystem:  1000,
			onlineCPUs: 2,
			want:       0,
		},
		{
			name:       "zero cpu delta",
			preCPU:     100,
			curCPU:     100,
			preSystem:  1000,
			curSystem:  2000,
			onlineCPUs: 1,
			want:       0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateCPUPercent(tt.preCPU, tt.curCPU, tt.preSystem, tt.curSystem, tt.onlineCPUs)
			diff := got - tt.want
			if diff < -0.01 || diff > 0.01 {
				t.Errorf("calculateCPUPercent() = %v, want %v", got, tt.want)
			}
		})
	}
}
