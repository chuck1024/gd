package network

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
)

// IsLocalIP, 检查是否为本地 IP
func IsLocalIP(request *http.Request) (bool, error) {
	ip, err := GetRealIP(request)
	if err != nil {
		return false, err
	}
	if ip == "127.0.0.1" {
		return true, nil
	}
	return false, nil
}

// GetRealIP, 获取 Request 的真实 IP
func GetRealIP(request *http.Request) (string, error) {
	var ip string

	// Nginx 中有的remoteAddr存为了"X-Real-IP"
	if len(ip) == 0 && len(request.Header.Get("X-Real-IP")) > 0 {
		xRealIP := request.Header.Get("X-Real-IP")
		if len(xRealIP) > 0 && net.ParseIP(xRealIP) != nil {
			ip = xRealIP
		}
	}

	if len(request.Header.Get("X-Forwarded-For")) > 0 {
		// Reference: http://en.wikipedia.org/wiki/X-Forwarded-For#Format
		xForwardedFor := strings.Split(request.Header.Get("X-Forwarded-For"), ", ")
		if len(xForwardedFor) > 0 && net.ParseIP(xForwardedFor[0]) != nil {
			ip = xForwardedFor[0]
		}
	}

	if len(ip) == 0 && len(request.RemoteAddr) > 0 {
		remoteAddr := strings.Split(request.RemoteAddr, ":")
		if len(remoteAddr) == 2 && net.ParseIP(remoteAddr[0]) != nil {
			ip = remoteAddr[0]
		}
	}

	if len(ip) == 0 {
		return "", errors.New(fmt.Sprintf("cannot get real ip from request %+v", request))
	}
	return ip, nil
}

// 获取本地IP
func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		os.Stderr.WriteString("Oops: " + err.Error() + "\n")
	} else {
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					return ipnet.IP.String()
				}
			}
		}
	}
	return ""
}
