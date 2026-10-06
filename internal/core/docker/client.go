package docker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var (
	ErrBinaryNotFound = errors.New("docker CLI binary not found on PATH")
	ErrDaemonOffline  = errors.New("docker daemon is offline or not responding")
)

// ContainerInfo represents the runtime status of a compose service container.
type ContainerInfo struct {
	Service     string `json:"service"`
	ContainerID string `json:"container_id"`
	State       string `json:"state"`  // "running", "exited", "created", "restarting", etc.
	Status      string `json:"status"` // e.g. "Up 2 hours", "Exited (0) 5 minutes ago"
	Ports       string `json:"ports"`
	Image       string `json:"image"`
	Names       string `json:"names,omitempty"`
}

// DockerStatus reports availability of the docker CLI and background daemon.
type DockerStatus struct {
	BinaryFound   bool   `json:"binary_found"`
	DaemonRunning bool   `json:"daemon_running"`
	Version       string `json:"version,omitempty"`
	Error         string `json:"error,omitempty"`
}

// Client executes docker compose CLI commands.
type Client struct {
	BinaryPath string
	Timeout    time.Duration
}

// NewClient returns a new Docker CLI client using default PATH lookup.
func NewClient() *Client {
	bin, err := exec.LookPath("docker")
	if err != nil {
		bin = "docker"
	}
	return &Client{
		BinaryPath: bin,
		Timeout:    30 * time.Second,
	}
}

// NewClientWithBinary returns a client using an explicit binary path.
func NewClientWithBinary(binPath string) *Client {
	return &Client{
		BinaryPath: binPath,
		Timeout:    30 * time.Second,
	}
}

func (c *Client) getBinary() string {
	if c.BinaryPath != "" {
		return c.BinaryPath
	}
	return "docker"
}

// IsBinaryAvailable checks if the docker binary exists on PATH or at BinaryPath.
func (c *Client) IsBinaryAvailable() bool {
	bin := c.getBinary()
	_, err := exec.LookPath(bin)
	return err == nil
}

// IsDaemonRunning checks if the docker daemon responds to info queries.
func (c *Client) IsDaemonRunning(ctx context.Context) bool {
	status := c.CheckStatus(ctx)
	return status.DaemonRunning
}

// CheckStatus verifies both binary presence and daemon responsiveness.
func (c *Client) CheckStatus(ctx context.Context) DockerStatus {
	bin := c.getBinary()
	path, err := exec.LookPath(bin)
	if err != nil {
		return DockerStatus{
			BinaryFound:   false,
			DaemonRunning: false,
			Error:         "docker CLI binary not found on PATH",
		}
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, path, "info", "--format", "{{.ServerVersion}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return DockerStatus{
			BinaryFound:   true,
			DaemonRunning: false,
			Error:         strings.TrimSpace(string(out)),
		}
	}

	version := strings.TrimSpace(string(out))
	return DockerStatus{
		BinaryFound:   true,
		DaemonRunning: true,
		Version:       version,
	}
}

// ListContainers queries container status for the given workspace directory.
func (c *Client) ListContainers(workspaceDir string) ([]ContainerInfo, error) {
	if !c.IsBinaryAvailable() {
		return nil, ErrBinaryNotFound
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	// Try JSON format first (docker compose ps --format json)
	cmd := exec.CommandContext(ctx, c.getBinary(), "compose", "ps", "-a", "--format", "json")
	if workspaceDir != "" {
		cmd.Dir = workspaceDir
	}

	out, err := cmd.CombinedOutput()
	outStr := string(out)

	if err != nil {
		// If daemon is not running, return error
		if strings.Contains(strings.ToLower(outStr), "cannot connect") ||
			strings.Contains(strings.ToLower(outStr), "daemon is not running") ||
			strings.Contains(strings.ToLower(outStr), "docker api at") {
			return nil, ErrDaemonOffline
		}
		// Try fallback without -a or without --format json
		fallbackCmd := exec.CommandContext(ctx, c.getBinary(), "compose", "ps")
		if workspaceDir != "" {
			fallbackCmd.Dir = workspaceDir
		}
		fbOut, fbErr := fallbackCmd.CombinedOutput()
		if fbErr != nil {
			return nil, fmt.Errorf("docker compose ps failed: %s (%w)", strings.TrimSpace(string(fbOut)), fbErr)
		}
		return ParseContainersTable(string(fbOut))
	}

	if strings.TrimSpace(outStr) == "" {
		return []ContainerInfo{}, nil
	}

	return ParseContainersOutput(outStr)
}

// rawJSONContainer captures fields emitted by modern Docker Compose v2 JSON output.
type rawJSONContainer struct {
	ID          string `json:"ID"`
	Id          string `json:"Id"`
	ContainerID string `json:"ContainerID"`
	Name        string `json:"Name"`
	Names       string `json:"Names"`
	Service     string `json:"Service"`
	State       string `json:"State"`
	Status      string `json:"Status"`
	Image       string `json:"Image"`
	Ports       any    `json:"Ports"`
	Publishers  []struct {
		URL           string `json:"URL"`
		TargetPort    int    `json:"TargetPort"`
		PublishedPort int    `json:"PublishedPort"`
		Protocol      string `json:"Protocol"`
	} `json:"Publishers"`
}

// ParseContainersOutput parses docker compose ps output from JSON array, NDJSON, or fallback table.
func ParseContainersOutput(output string) ([]ContainerInfo, error) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return []ContainerInfo{}, nil
	}

	// 1. Try parsing as JSON array
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		var rawList []rawJSONContainer
		if err := json.Unmarshal([]byte(trimmed), &rawList); err == nil {
			var result []ContainerInfo
			for _, r := range rawList {
				result = append(result, r.toContainerInfo())
			}
			return result, nil
		}
	}

	// 2. Try parsing as NDJSON (newline-delimited JSON objects)
	if strings.HasPrefix(trimmed, "{") {
		var result []ContainerInfo
		lines := strings.Split(trimmed, "\n")
		parsedAny := false
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || !strings.HasPrefix(line, "{") {
				continue
			}
			var raw rawJSONContainer
			if err := json.Unmarshal([]byte(line), &raw); err == nil {
				result = append(result, raw.toContainerInfo())
				parsedAny = true
			}
		}
		if parsedAny {
			return result, nil
		}
	}

	// 3. Fallback to table parsing
	return ParseContainersTable(trimmed)
}

func (r rawJSONContainer) toContainerInfo() ContainerInfo {
	id := r.ID
	if id == "" {
		id = r.Id
	}
	if id == "" {
		id = r.ContainerID
	}
	if len(id) > 12 {
		id = id[:12]
	}

	svc := r.Service
	if svc == "" {
		svc = r.Name
	}

	state := strings.ToLower(strings.TrimSpace(r.State))
	status := strings.TrimSpace(r.Status)
	if state == "" {
		if strings.HasPrefix(status, "Up") {
			state = "running"
		} else if strings.HasPrefix(status, "Exited") {
			state = "exited"
		} else if strings.HasPrefix(status, "Created") {
			state = "created"
		} else {
			state = "stopped"
		}
	}

	// Format ports
	var portsStr string
	if len(r.Publishers) > 0 {
		var parts []string
		for _, pub := range r.Publishers {
			proto := pub.Protocol
			if proto == "" {
				proto = "tcp"
			}
			if pub.PublishedPort > 0 && pub.TargetPort > 0 {
				parts = append(parts, fmt.Sprintf("%d:%d/%s", pub.PublishedPort, pub.TargetPort, proto))
			} else if pub.TargetPort > 0 {
				parts = append(parts, fmt.Sprintf("%d/%s", pub.TargetPort, proto))
			}
		}
		portsStr = strings.Join(parts, ", ")
	} else if r.Ports != nil {
		switch p := r.Ports.(type) {
		case string:
			portsStr = p
		case []any:
			var strParts []string
			for _, it := range p {
				strParts = append(strParts, fmt.Sprint(it))
			}
			portsStr = strings.Join(strParts, ", ")
		}
	}

	name := r.Name
	if name == "" {
		name = r.Names
	}

	return ContainerInfo{
		Service:     svc,
		ContainerID: id,
		State:       state,
		Status:      status,
		Ports:       portsStr,
		Image:       r.Image,
		Names:       name,
	}
}

var multiSpaceRegex = regexp.MustCompile(`\s{2,}`)

// ParseContainersTable extracts ContainerInfo entries from standard tabular `docker compose ps` output.
func ParseContainersTable(table string) ([]ContainerInfo, error) {
	lines := strings.Split(table, "\n")
	if len(lines) == 0 {
		return []ContainerInfo{}, nil
	}

	headerIdx := -1
	for i, l := range lines {
		upper := strings.ToUpper(l)
		if strings.Contains(upper, "SERVICE") || strings.Contains(upper, "NAME") || strings.Contains(upper, "COMMAND") {
			headerIdx = i
			break
		}
	}

	if headerIdx == -1 {
		return []ContainerInfo{}, nil
	}

	headerLine := lines[headerIdx]
	headers := multiSpaceRegex.Split(headerLine, -1)
	colMap := make(map[string]int)
	for idx, h := range headers {
		colMap[strings.ToUpper(strings.TrimSpace(h))] = idx
	}

	var results []ContainerInfo
	for i := headerIdx + 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "---") {
			continue
		}

		cols := multiSpaceRegex.Split(line, -1)
		if len(cols) == 0 {
			continue
		}

		info := ContainerInfo{}
		if idx, ok := colMap["SERVICE"]; ok && idx < len(cols) {
			info.Service = strings.TrimSpace(cols[idx])
		}
		if info.Service == "" {
			if idx, ok := colMap["NAME"]; ok && idx < len(cols) {
				info.Service = strings.TrimSpace(cols[idx])
				info.Names = info.Service
			}
		}
		if idx, ok := colMap["IMAGE"]; ok && idx < len(cols) {
			info.Image = strings.TrimSpace(cols[idx])
		}
		if idx, ok := colMap["STATUS"]; ok && idx < len(cols) {
			info.Status = strings.TrimSpace(cols[idx])
			st := strings.ToLower(info.Status)
			if strings.HasPrefix(st, "up") {
				info.State = "running"
			} else if strings.HasPrefix(st, "exited") {
				info.State = "exited"
			} else if strings.HasPrefix(st, "created") {
				info.State = "created"
			} else {
				info.State = "stopped"
			}
		}
		if idx, ok := colMap["PORTS"]; ok && idx < len(cols) {
			info.Ports = strings.TrimSpace(cols[idx])
		}
		if idx, ok := colMap["CONTAINER ID"]; ok && idx < len(cols) {
			info.ContainerID = strings.TrimSpace(cols[idx])
		}

		if info.Service != "" {
			results = append(results, info)
		}
	}

	return results, nil
}

// Up runs `docker compose up -d` for all services or a specific service.
func (c *Client) Up(workspaceDir, service string) error {
	if !c.IsBinaryAvailable() {
		return ErrBinaryNotFound
	}

	args := []string{"compose", "up", "-d"}
	if service != "" {
		args = append(args, service)
	}

	cmd := exec.Command(c.getBinary(), args...)
	if workspaceDir != "" {
		cmd.Dir = workspaceDir
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose up: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// Down runs `docker compose down` to stop and remove containers and networks.
func (c *Client) Down(workspaceDir string) error {
	if !c.IsBinaryAvailable() {
		return ErrBinaryNotFound
	}

	cmd := exec.Command(c.getBinary(), "compose", "down")
	if workspaceDir != "" {
		cmd.Dir = workspaceDir
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose down: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// Restart restarts all or a specific service container.
func (c *Client) Restart(workspaceDir, service string) error {
	if !c.IsBinaryAvailable() {
		return ErrBinaryNotFound
	}

	args := []string{"compose", "restart"}
	if service != "" {
		args = append(args, service)
	}

	cmd := exec.Command(c.getBinary(), args...)
	if workspaceDir != "" {
		cmd.Dir = workspaceDir
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose restart: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// StreamLogs streams real-time logs from docker compose with cancellation context.
func (c *Client) StreamLogs(ctx context.Context, workspaceDir, service string, onLine func(string)) error {
	if !c.IsBinaryAvailable() {
		return ErrBinaryNotFound
	}

	args := []string{"compose", "logs", "-f", "--tail=100"}
	if service != "" {
		args = append(args, service)
	}

	cmd := exec.CommandContext(ctx, c.getBinary(), args...)
	if workspaceDir != "" {
		cmd.Dir = workspaceDir
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting logs stream: %w", err)
	}

	reader := io.MultiReader(stdoutPipe, stderrPipe)
	scanner := bufio.NewScanner(reader)
	// Buffer up to 1MB per line for long log entries
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	doneCh := make(chan struct{})
	go func() {
		for scanner.Scan() {
			text := scanner.Text()
			if onLine != nil {
				onLine(text)
			}
		}
		close(doneCh)
	}()

	select {
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-doneCh
		return ctx.Err()
	case <-doneCh:
		_ = cmd.Wait()
		return nil
	}
}

// ExecCommand returns the command arguments to launch an interactive shell in the service container.
func (c *Client) ExecCommand(workspaceDir, service string) []string {
	bin := c.getBinary()
	return []string{bin, "compose", "exec", service, "sh"}
}

// GetServicesWithCompose retrieves runtime containers and unions them with services parsed from compose.yaml.
func (c *Client) GetServicesWithCompose(workspaceDir string) ([]ContainerInfo, error) {
	// Parse compose yaml if available
	var composeProject *ComposeProject
	if workspaceDir != "" {
		if proj, err := ParseComposeFile(workspaceDir); err == nil {
			composeProject = proj
		}
	}

	// Try listing runtime containers
	containers, listErr := c.ListContainers(workspaceDir)
	if listErr != nil && composeProject == nil {
		return nil, listErr
	}

	knownMap := make(map[string]ContainerInfo)
	for _, ci := range containers {
		knownMap[ci.Service] = ci
	}

	var combined []ContainerInfo
	if composeProject != nil {
		// Maintain order or add services defined in YAML
		for sName, sCfg := range composeProject.Services {
			if existing, ok := knownMap[sName]; ok {
				if existing.Image == "" && sCfg.Image != "" {
					existing.Image = sCfg.Image
				}
				if existing.Ports == "" && len(sCfg.Ports) > 0 {
					existing.Ports = strings.Join(sCfg.Ports, ", ")
				}
				combined = append(combined, existing)
				delete(knownMap, sName)
			} else {
				// Not running
				ports := ""
				if len(sCfg.Ports) > 0 {
					ports = strings.Join(sCfg.Ports, ", ")
				}
				combined = append(combined, ContainerInfo{
					Service: sName,
					State:   "exited",
					Status:  "Stopped",
					Ports:   ports,
					Image:   sCfg.Image,
				})
			}
		}
	}

	// Add any other running containers not explicitly in compose file
	for _, remaining := range knownMap {
		combined = append(combined, remaining)
	}

	return combined, nil
}
