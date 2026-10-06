package docker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const sampleComposeYAML = `
version: "3.8"

services:
  web:
    image: "nginx:alpine"
    container_name: web_frontend
    build: .
    ports:
      - "8080:80"
      - "443:443"
    environment:
      - NODE_ENV=production
      - PORT=8080
      - DATABASE_URL=postgres://user:secret@db:5432/app?sslmode=disable
    volumes:
      - ./html:/usr/share/nginx/html:ro
      - web_logs:/var/log/nginx
    command: ["nginx", "-g", "daemon off;"]

  db:
    image: postgres:15-alpine
    container_name: db_postgres
    build:
      context: ./database
      dockerfile: Dockerfile.postgres
    ports: [ "5432:5432" ]
    environment:
      POSTGRES_DB: myapp
      POSTGRES_USER: admin
      POSTGRES_PASSWORD: supersecretpassword
    volumes:
      - pgdata:/var/lib/postgresql/data
    command: postgres -c shared_buffers=256MB

  redis:
    image: "redis:7-alpine"
    ports:
      - 6379:6379
`

func TestComposeParser_StandardYAML(t *testing.T) {
	proj, err := ParseComposeYAML(sampleComposeYAML)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	if proj.Version != "3.8" {
		t.Errorf("expected version 3.8, got %q", proj.Version)
	}

	if len(proj.Services) != 3 {
		t.Fatalf("expected 3 services, got %d", len(proj.Services))
	}

	// Verify 'web'
	web, ok := proj.Services["web"]
	if !ok {
		t.Fatalf("service 'web' not found")
	}
	if web.Image != "nginx:alpine" {
		t.Errorf("web.Image = %q, want 'nginx:alpine'", web.Image)
	}
	if web.ContainerName != "web_frontend" {
		t.Errorf("web.ContainerName = %q, want 'web_frontend'", web.ContainerName)
	}
	if web.Build != "." {
		t.Errorf("web.Build = %q, want '.'", web.Build)
	}
	if len(web.Ports) != 2 || web.Ports[0] != "8080:80" || web.Ports[1] != "443:443" {
		t.Errorf("web.Ports = %v, want ['8080:80', '443:443']", web.Ports)
	}
	if web.Environment["NODE_ENV"] != "production" {
		t.Errorf("web.Environment[NODE_ENV] = %q, want 'production'", web.Environment["NODE_ENV"])
	}
	if web.Environment["PORT"] != "8080" {
		t.Errorf("web.Environment[PORT] = %q, want '8080'", web.Environment["PORT"])
	}
	if web.Environment["DATABASE_URL"] != "postgres://user:secret@db:5432/app?sslmode=disable" {
		t.Errorf("web.Environment[DATABASE_URL] = %q", web.Environment["DATABASE_URL"])
	}
	if len(web.Volumes) != 2 {
		t.Errorf("web.Volumes len = %d, want 2", len(web.Volumes))
	}
	if web.Command != "nginx -g daemon off;" {
		t.Errorf("web.Command = %q, want 'nginx -g daemon off;'", web.Command)
	}

	// Verify 'db'
	db, ok := proj.Services["db"]
	if !ok {
		t.Fatalf("service 'db' not found")
	}
	if db.Image != "postgres:15-alpine" {
		t.Errorf("db.Image = %q, want 'postgres:15-alpine'", db.Image)
	}
	if db.BuildContext != "./database" {
		t.Errorf("db.BuildContext = %q, want './database'", db.BuildContext)
	}
	if db.Dockerfile != "Dockerfile.postgres" {
		t.Errorf("db.Dockerfile = %q, want 'Dockerfile.postgres'", db.Dockerfile)
	}
	if len(db.Ports) != 1 || db.Ports[0] != "5432:5432" {
		t.Errorf("db.Ports = %v, want ['5432:5432']", db.Ports)
	}
	if db.Environment["POSTGRES_DB"] != "myapp" {
		t.Errorf("db.Environment[POSTGRES_DB] = %q, want 'myapp'", db.Environment["POSTGRES_DB"])
	}
	if db.Environment["POSTGRES_USER"] != "admin" {
		t.Errorf("db.Environment[POSTGRES_USER] = %q, want 'admin'", db.Environment["POSTGRES_USER"])
	}
	if db.Command != "postgres -c shared_buffers=256MB" {
		t.Errorf("db.Command = %q", db.Command)
	}

	// Verify 'redis'
	redis, ok := proj.Services["redis"]
	if !ok {
		t.Fatalf("service 'redis' not found")
	}
	if redis.Image != "redis:7-alpine" {
		t.Errorf("redis.Image = %q, want 'redis:7-alpine'", redis.Image)
	}
	if len(redis.Ports) != 1 || redis.Ports[0] != "6379:6379" {
		t.Errorf("redis.Ports = %v, want ['6379:6379']", redis.Ports)
	}
}

func TestComposeParser_FindAndParseFile(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Initially no file
	_, err := FindComposeFile(tempDir)
	if err == nil {
		t.Fatalf("expected error when no compose file exists")
	}

	// 2. Create compose.yaml
	filePath := filepath.Join(tempDir, "compose.yaml")
	if err := os.WriteFile(filePath, []byte(sampleComposeYAML), 0644); err != nil {
		t.Fatalf("writing temp compose file: %v", err)
	}

	found, err := FindComposeFile(tempDir)
	if err != nil {
		t.Fatalf("FindComposeFile error: %v", err)
	}
	if found != filePath {
		t.Errorf("found %q, want %q", found, filePath)
	}

	// 3. Parse via directory
	proj, err := ParseComposeFile(tempDir)
	if err != nil {
		t.Fatalf("ParseComposeFile error: %v", err)
	}
	if len(proj.Services) != 3 {
		t.Errorf("expected 3 services, got %d", len(proj.Services))
	}
	if proj.FilePath != filePath {
		t.Errorf("project.FilePath = %q, want %q", proj.FilePath, filePath)
	}
}

func TestClient_ParseContainersJSON_Array(t *testing.T) {
	jsonInput := `[
		{
			"ID": "a1b2c3d4e5f67890",
			"Name": "tahr-web-1",
			"Service": "web",
			"State": "running",
			"Status": "Up 3 hours",
			"Image": "nginx:alpine",
			"Publishers": [
				{
					"URL": "0.0.0.0",
					"TargetPort": 80,
					"PublishedPort": 8080,
					"Protocol": "tcp"
				}
			]
		},
		{
			"ID": "f6e5d4c3b2a11234",
			"Name": "tahr-db-1",
			"Service": "db",
			"State": "exited",
			"Status": "Exited (0) 10 minutes ago",
			"Image": "postgres:15-alpine",
			"Publishers": []
		}
	]`

	containers, err := ParseContainersOutput(jsonInput)
	if err != nil {
		t.Fatalf("unexpected error parsing JSON array: %v", err)
	}

	if len(containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(containers))
	}

	web := containers[0]
	if web.Service != "web" {
		t.Errorf("web.Service = %q, want 'web'", web.Service)
	}
	if web.ContainerID != "a1b2c3d4e5f6" {
		t.Errorf("web.ContainerID = %q, want 12 chars 'a1b2c3d4e5f6'", web.ContainerID)
	}
	if web.State != "running" {
		t.Errorf("web.State = %q, want 'running'", web.State)
	}
	if web.Status != "Up 3 hours" {
		t.Errorf("web.Status = %q, want 'Up 3 hours'", web.Status)
	}
	if web.Ports != "8080:80/tcp" {
		t.Errorf("web.Ports = %q, want '8080:80/tcp'", web.Ports)
	}
	if web.Image != "nginx:alpine" {
		t.Errorf("web.Image = %q, want 'nginx:alpine'", web.Image)
	}

	db := containers[1]
	if db.Service != "db" {
		t.Errorf("db.Service = %q, want 'db'", db.Service)
	}
	if db.State != "exited" {
		t.Errorf("db.State = %q, want 'exited'", db.State)
	}
}

func TestClient_ParseContainersJSON_NDJSON(t *testing.T) {
	ndjson := `{"ID":"111122223333","Service":"api","State":"running","Status":"Up 1 hour","Ports":"3000:3000","Image":"node:18"}
{"ID":"444455556666","Service":"worker","State":"created","Status":"Created","Ports":"","Image":"worker:latest"}`

	containers, err := ParseContainersOutput(ndjson)
	if err != nil {
		t.Fatalf("unexpected error parsing NDJSON: %v", err)
	}

	if len(containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(containers))
	}

	if containers[0].Service != "api" || containers[0].State != "running" || containers[0].Ports != "3000:3000" {
		t.Errorf("containers[0] = %+v", containers[0])
	}
	if containers[1].Service != "worker" || containers[1].State != "created" {
		t.Errorf("containers[1] = %+v", containers[1])
	}
}

func TestClient_ParseContainersTable(t *testing.T) {
	tableOutput := `NAME                IMAGE               COMMAND                  SERVICE             STATUS              PORTS
tahr-web-1          nginx:alpine        "/docker-entrypoint…"    web                 Up 5 hours          0.0.0.0:8080->80/tcp
tahr-db-1           postgres:15         "docker-entrypoint.s…"   db                  Exited (1) 2m ago   5432/tcp
`

	containers, err := ParseContainersTable(tableOutput)
	if err != nil {
		t.Fatalf("table parse error: %v", err)
	}

	if len(containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(containers))
	}

	if containers[0].Service != "web" || containers[0].State != "running" {
		t.Errorf("containers[0] = %+v", containers[0])
	}
	if containers[1].Service != "db" || containers[1].State != "exited" {
		t.Errorf("containers[1] = %+v", containers[1])
	}
}

func TestClient_OfflineGraceful(t *testing.T) {
	// 1. Client with a nonexistent binary
	client := NewClientWithBinary("nonexistent-docker-binary-xyz")
	if client.IsBinaryAvailable() {
		t.Fatalf("expected IsBinaryAvailable() to be false for nonexistent binary")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	status := client.CheckStatus(ctx)
	if status.BinaryFound {
		t.Errorf("expected BinaryFound to be false, got true")
	}
	if status.DaemonRunning {
		t.Errorf("expected DaemonRunning to be false, got true")
	}

	_, err := client.ListContainers("")
	if err == nil {
		t.Fatalf("expected error from ListContainers when binary missing")
	}

	// 2. Client with default docker on system where daemon is offline
	defaultClient := NewClient()
	// Should not crash even if daemon is not running
	stat := defaultClient.CheckStatus(ctx)
	t.Logf("Host docker status: binary_found=%v, daemon_running=%v, error=%s",
		stat.BinaryFound, stat.DaemonRunning, stat.Error)
}

func TestClient_ExecCommand(t *testing.T) {
	client := NewClientWithBinary("docker")
	cmdArgs := client.ExecCommand("/path/to/workspace", "backend")

	expected := []string{"docker", "compose", "exec", "backend", "sh"}
	if len(cmdArgs) != len(expected) {
		t.Fatalf("got %v, want %v", cmdArgs, expected)
	}
	for i := range expected {
		if cmdArgs[i] != expected[i] {
			t.Errorf("cmdArgs[%d] = %q, want %q", i, cmdArgs[i], expected[i])
		}
	}
}

func TestClient_ServicesMerge(t *testing.T) {
	tempDir := t.TempDir()
	composePath := filepath.Join(tempDir, "compose.yaml")
	if err := os.WriteFile(composePath, []byte(sampleComposeYAML), 0644); err != nil {
		t.Fatalf("writing temp compose: %v", err)
	}

	client := NewClientWithBinary("nonexistent-docker")
	// Since docker is nonexistent, it should gracefully fall back to compose YAML services
	services, err := client.GetServicesWithCompose(tempDir)
	if err != nil {
		t.Fatalf("unexpected error from GetServicesWithCompose: %v", err)
	}

	if len(services) != 3 {
		t.Fatalf("expected 3 services merged from YAML, got %d", len(services))
	}

	for _, s := range services {
		if s.State != "exited" {
			t.Errorf("expected offline service state 'exited', got %q", s.State)
		}
		if s.Image == "" {
			t.Errorf("expected image to be populated from YAML for service %s", s.Service)
		}
	}
}
