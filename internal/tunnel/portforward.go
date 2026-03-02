package tunnel

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

type ConnectivityResult struct {
	Port80  bool   `json:"port_80"`
	Port443 bool   `json:"port_443"`
	Message string `json:"message"`
}

func CheckConnectivity(ctx context.Context) ConnectivityResult {
	result := ConnectivityResult{}

	result.Port80 = checkPort(ctx, 80)
	result.Port443 = checkPort(ctx, 443)

	if result.Port80 && result.Port443 {
		result.Message = "Both ports 80 and 443 are accessible"
	} else if !result.Port80 && !result.Port443 {
		result.Message = "Neither port 80 nor 443 is accessible. Configure port forwarding on your router or use a Cloudflare tunnel."
	} else {
		result.Message = fmt.Sprintf("Port 80: %v, Port 443: %v", result.Port80, result.Port443)
	}

	return result
}

func checkPort(ctx context.Context, port int) bool {
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return true
	}
	listener.Close()
	return false
}

func ConnectivityHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		result := CheckConnectivity(ctx)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"port_80":%v,"port_443":%v,"message":"%s"}`, result.Port80, result.Port443, result.Message)
	}
}
