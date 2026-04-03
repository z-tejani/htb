package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type VPNManager struct {
	baseDir string
}

type VPNRuntimeStatus struct {
	Connected  bool
	Available  bool
	OpenVPNBin string
	ServerID   int
	ServerName string
	ConfigPath string
	LogPath    string
	PID        int
}

type vpnMetadata struct {
	ServerID   int    `json:"server_id"`
	ServerName string `json:"server_name"`
	ConfigPath string `json:"config_path"`
	LogPath    string `json:"log_path"`
}

func NewVPNManager(baseDir string) VPNManager {
	return VPNManager{baseDir: baseDir}
}

func (m VPNManager) DownloadConfig(client *HTBClient, server VPNServer) (string, error) {
	if !server.Assigned {
		if err := client.SwitchVPN(server.ID); err != nil {
			return "", err
		}
	}

	payload, filename, err := client.DownloadVPNProfile(server.ID)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(m.stateDir(), 0o755); err != nil {
		return "", err
	}

	path := filepath.Join(m.stateDir(), safeVPNFilename(filename))
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return "", err
	}

	return path, nil
}

func (m VPNManager) Connect(client *HTBClient, server VPNServer) (VPNRuntimeStatus, error) {
	bin, err := exec.LookPath("openvpn")
	if err != nil {
		return VPNRuntimeStatus{}, fmt.Errorf("openvpn is not installed")
	}

	status, _ := m.Status()
	if status.Connected {
		if err := m.disconnectPID(status.PID); err != nil {
			return status, err
		}
	}

	configPath, err := m.DownloadConfig(client, server)
	if err != nil {
		return VPNRuntimeStatus{}, err
	}

	if err := os.MkdirAll(m.stateDir(), 0o755); err != nil {
		return VPNRuntimeStatus{}, err
	}

	_ = os.Remove(m.pidPath())
	logPath := filepath.Join(m.stateDir(), "openvpn.log")
	cmd := exec.Command(bin,
		"--config", configPath,
		"--daemon", "htbtui",
		"--writepid", m.pidPath(),
		"--log", logPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return VPNRuntimeStatus{}, fmt.Errorf("openvpn failed to start: %s", strings.TrimSpace(string(output)))
	}

	meta := vpnMetadata{
		ServerID:   server.ID,
		ServerName: server.Name,
		ConfigPath: configPath,
		LogPath:    logPath,
	}
	if err := m.writeMetadata(meta); err != nil {
		return VPNRuntimeStatus{}, err
	}

	time.Sleep(1500 * time.Millisecond)
	status, err = m.Status()
	if err != nil {
		return status, err
	}
	if !status.Connected {
		return status, fmt.Errorf("openvpn did not stay connected; check %s", logPath)
	}

	return status, nil
}

func (m VPNManager) Disconnect() (VPNRuntimeStatus, error) {
	status, err := m.Status()
	if err != nil {
		return status, err
	}
	if !status.Connected || status.PID == 0 {
		_ = os.Remove(m.pidPath())
		return status, nil
	}

	if err := m.disconnectPID(status.PID); err != nil {
		return status, err
	}

	_ = os.Remove(m.pidPath())
	return m.Status()
}

func (m VPNManager) Status() (VPNRuntimeStatus, error) {
	status := VPNRuntimeStatus{}

	if bin, err := exec.LookPath("openvpn"); err == nil {
		status.Available = true
		status.OpenVPNBin = bin
	}

	meta, _ := m.readMetadata()
	status.ServerID = meta.ServerID
	status.ServerName = meta.ServerName
	status.ConfigPath = meta.ConfigPath
	status.LogPath = meta.LogPath

	pidBytes, err := os.ReadFile(m.pidPath())
	if err != nil {
		if !os.IsNotExist(err) {
			return status, err
		}
		return status, nil
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		_ = os.Remove(m.pidPath())
		return status, nil
	}

	if processExists(pid) {
		status.Connected = true
		status.PID = pid
		return status, nil
	}

	_ = os.Remove(m.pidPath())
	return status, nil
}

func (m VPNManager) disconnectPID(pid int) error {
	if pid == 0 {
		return nil
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		return err
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("timed out disconnecting vpn process %d", pid)
}

func (m VPNManager) stateDir() string {
	return filepath.Join(m.baseDir, ".htbtui", "vpn")
}

func (m VPNManager) pidPath() string {
	return filepath.Join(m.stateDir(), "openvpn.pid")
}

func (m VPNManager) metadataPath() string {
	return filepath.Join(m.stateDir(), "connection.json")
}

func (m VPNManager) writeMetadata(meta vpnMetadata) error {
	if err := os.MkdirAll(m.stateDir(), 0o755); err != nil {
		return err
	}

	payload, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.metadataPath(), payload, 0o600)
}

func (m VPNManager) readMetadata() (vpnMetadata, error) {
	payload, err := os.ReadFile(m.metadataPath())
	if err != nil {
		return vpnMetadata{}, err
	}

	var meta vpnMetadata
	if err := json.Unmarshal(payload, &meta); err != nil {
		return vpnMetadata{}, err
	}

	return meta, nil
}

func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func safeVPNFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." || name == "/" {
		return "htb.ovpn"
	}

	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}

	result := b.String()
	if result == "" {
		return "htb.ovpn"
	}
	return result
}
