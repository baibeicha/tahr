package grpcproto

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleProto3 = `
syntax = "proto3";

package test.service.v1;

option go_package = "test/service/v1;servicev1";
option java_multiple_files = true;

import "google/protobuf/timestamp.proto";
import "google/protobuf/empty.proto";

enum UserStatus {
  UNKNOWN = 0;
  ACTIVE = 1;
  SUSPENDED = 2;
  DELETED = 3;
}

message UserProfile {
  string user_id = 1;
  string email = 2;
  string display_name = 3;
  int32 age = 4;
  bool is_verified = 5;
  repeated string roles = 6;
  map<string, string> metadata = 7;
  UserStatus status = 8;
  google.protobuf.Timestamp created_at = 9;

  message Location {
    string city = 1;
    string country = 2;
    double latitude = 3;
    double longitude = 4;
  }
  Location location = 10;

  oneof contact_method {
    string phone_number = 11;
    string alternate_email = 12;
  }
}

message GetUserRequest {
  string user_id = 1;
}

message StreamUsersRequest {
  repeated string user_ids = 1;
  int32 batch_size = 2;
}

service UserService {
  rpc GetUser (GetUserRequest) returns (UserProfile);
  rpc CreateUser (UserProfile) returns (UserProfile);
  rpc StreamUsers (StreamUsersRequest) returns (stream UserProfile);
  rpc Chat (stream UserProfile) returns (stream UserProfile);
  rpc DeleteUser (GetUserRequest) returns (google.protobuf.Empty);
}
`

const sampleProto2 = `
syntax = "proto2";

package legacy.pkg;

message LegacyItem {
  required int64 item_id = 1;
  optional string item_name = 2 [default = "default_item"];
  repeated int32 tags = 3;
}
`

func TestProtoParser_Proto3Syntax(t *testing.T) {
	pf, err := ParseProtoString(sampleProto3, "test.proto")
	if err != nil {
		t.Fatalf("failed to parse proto3: %v", err)
	}

	if pf.Syntax != "proto3" {
		t.Errorf("expected syntax proto3, got %s", pf.Syntax)
	}
	if pf.Package != "test.service.v1" {
		t.Errorf("expected package test.service.v1, got %s", pf.Package)
	}
	if len(pf.Imports) != 2 {
		t.Errorf("expected 2 imports, got %d", len(pf.Imports))
	}
	if len(pf.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(pf.Services))
	}

	svc := pf.Services[0]
	if svc.Name != "UserService" {
		t.Errorf("expected service name UserService, got %s", svc.Name)
	}
	if len(svc.Methods) != 5 {
		t.Fatalf("expected 5 RPC methods, got %d", len(svc.Methods))
	}

	// Verify RPCs
	rpc1 := svc.Methods[0]
	if rpc1.Name != "GetUser" || rpc1.RequestType != "GetUserRequest" || rpc1.ResponseType != "UserProfile" {
		t.Errorf("unexpected RPC 1: %+v", rpc1)
	}
	if rpc1.ClientStreaming || rpc1.ServerStreaming {
		t.Errorf("rpc1 should not be streaming")
	}

	rpc3 := svc.Methods[2] // StreamUsers -> stream UserProfile
	if rpc3.Name != "StreamUsers" || rpc3.ClientStreaming || !rpc3.ServerStreaming {
		t.Errorf("expected server-streaming RPC, got %+v", rpc3)
	}

	rpc4 := svc.Methods[3] // stream UserProfile -> stream UserProfile
	if rpc4.Name != "Chat" || !rpc4.ClientStreaming || !rpc4.ServerStreaming {
		t.Errorf("expected bidirectional-streaming RPC, got %+v", rpc4)
	}

	// Verify Messages
	if len(pf.Messages) != 3 {
		t.Fatalf("expected 3 top-level messages, got %d", len(pf.Messages))
	}

	userProfileMsg := pf.Messages[0]
	if userProfileMsg.Name != "UserProfile" {
		t.Errorf("expected UserProfile message, got %s", userProfileMsg.Name)
	}
	if len(userProfileMsg.NestedMessages) != 1 {
		t.Errorf("expected 1 nested message in UserProfile, got %d", len(userProfileMsg.NestedMessages))
	}

	// Verify Enums
	if len(pf.Enums) != 1 {
		t.Fatalf("expected 1 enum, got %d", len(pf.Enums))
	}
	en := pf.Enums[0]
	if en.Name != "UserStatus" || len(en.Values) != 4 {
		t.Errorf("unexpected enum: %+v", en)
	}
}

func TestProtoParser_Proto2Syntax(t *testing.T) {
	pf, err := ParseProtoString(sampleProto2, "legacy.proto")
	if err != nil {
		t.Fatalf("failed to parse proto2: %v", err)
	}

	if pf.Syntax != "proto2" {
		t.Errorf("expected syntax proto2, got %s", pf.Syntax)
	}
	if len(pf.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(pf.Messages))
	}

	msg := pf.Messages[0]
	if len(msg.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(msg.Fields))
	}

	f1 := msg.Fields[0]
	if !f1.IsRequired || f1.Name != "item_id" || f1.Type != "int64" || f1.Number != 1 {
		t.Errorf("unexpected f1: %+v", f1)
	}

	f2 := msg.Fields[1]
	if !f2.IsOptional || f2.DefaultValue != "default_item" {
		t.Errorf("unexpected f2: %+v", f2)
	}

	f3 := msg.Fields[2]
	if !f3.IsRepeated || f3.Name != "tags" || f3.Type != "int32" {
		t.Errorf("unexpected f3: %+v", f3)
	}
}

func TestProtoParser_FieldExtraction(t *testing.T) {
	pf, err := ParseProtoString(sampleProto3, "test.proto")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	msg := pf.Messages[0] // UserProfile
	fieldMap := make(map[string]Field)
	for _, f := range msg.Fields {
		fieldMap[f.Name] = f
	}

	// string user_id = 1
	if f, ok := fieldMap["user_id"]; !ok || f.Type != "string" || f.Number != 1 {
		t.Errorf("invalid user_id field: %+v", f)
	}

	// repeated string roles = 6
	if f, ok := fieldMap["roles"]; !ok || !f.IsRepeated || f.Type != "string" || f.Number != 6 {
		t.Errorf("invalid roles field: %+v", f)
	}

	// map<string, string> metadata = 7
	if f, ok := fieldMap["metadata"]; !ok || !f.IsMap || f.MapKeyType != "string" || f.MapValueType != "string" {
		t.Errorf("invalid metadata map field: %+v", f)
	}

	// oneof contact_method: phone_number & alternate_email
	if f, ok := fieldMap["phone_number"]; !ok || f.OneOfName != "contact_method" {
		t.Errorf("invalid oneof phone_number field: %+v", f)
	}
	if f, ok := fieldMap["alternate_email"]; !ok || f.OneOfName != "contact_method" {
		t.Errorf("invalid oneof alternate_email field: %+v", f)
	}
}

func TestMockBuilder_Synthesis(t *testing.T) {
	pf, err := ParseProtoString(sampleProto3, "test.proto")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	ws := NewProtoWorkspace(".")
	ws.AddFile(pf)

	mb := NewMockBuilder(ws)

	// 1. Synthesize GetUserRequest mock
	reqJSON, err := mb.BuildMessageMockJSON("GetUserRequest")
	if err != nil {
		t.Fatalf("failed to build GetUserRequest mock: %v", err)
	}

	if !json.Valid([]byte(reqJSON)) {
		t.Fatalf("generated JSON is invalid: %s", reqJSON)
	}

	var reqData map[string]interface{}
	if err := json.Unmarshal([]byte(reqJSON), &reqData); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}
	if reqData["user_id"] != "usr_1001" {
		t.Errorf("expected user_id heuristic 'usr_1001', got %v", reqData["user_id"])
	}

	// 2. Synthesize UserProfile complex mock
	profileJSON, err := mb.BuildMessageMockJSON("UserProfile")
	if err != nil {
		t.Fatalf("failed to build UserProfile mock: %v", err)
	}

	if !json.Valid([]byte(profileJSON)) {
		t.Fatalf("generated UserProfile JSON is invalid: %s", profileJSON)
	}

	var profData map[string]interface{}
	if err := json.Unmarshal([]byte(profileJSON), &profData); err != nil {
		t.Fatalf("failed to unmarshal profile JSON: %v", err)
	}

	// Type-aware heuristics
	if profData["email"] != "user@example.com" {
		t.Errorf("expected email 'user@example.com', got %v", profData["email"])
	}
	if profData["is_verified"] != true {
		t.Errorf("expected bool true, got %v", profData["is_verified"])
	}

	// Repeated field: roles -> array
	roles, ok := profData["roles"].([]interface{})
	if !ok || len(roles) != 1 {
		t.Errorf("expected roles to be 1-element array, got %v", profData["roles"])
	}

	// Map field: metadata -> map
	meta, ok := profData["metadata"].(map[string]interface{})
	if !ok || len(meta) == 0 {
		t.Errorf("expected metadata map, got %v", profData["metadata"])
	}

	// Enum field: UserStatus -> ACTIVE (non-zero value preferred)
	status, ok := profData["status"].(string)
	if !ok || (status != "ACTIVE" && status != "UNKNOWN") {
		t.Errorf("expected valid enum string name, got %v", profData["status"])
	}

	// Nested message: location
	loc, ok := profData["location"].(map[string]interface{})
	if !ok || loc["city"] == nil {
		t.Errorf("expected nested location object, got %v", profData["location"])
	}

	// OneOf: only 1 of the fields should be present in mock
	hasPhone := profData["phone_number"] != nil
	hasAltEmail := profData["alternate_email"] != nil
	if !(hasPhone != hasAltEmail) {
		t.Errorf("expected exactly one oneof field populated, got phone=%v alt_email=%v", hasPhone, hasAltEmail)
	}
}

func TestMockBuilder_WellKnownTypes(t *testing.T) {
	ws := NewProtoWorkspace(".")
	mb := NewMockBuilder(ws)

	// Timestamp
	tsJSON, err := mb.BuildMessageMockJSON("google.protobuf.Timestamp")
	if err != nil || !json.Valid([]byte(tsJSON)) {
		t.Fatalf("failed to synthesize Timestamp: %v", err)
	}
	if !strings.Contains(tsJSON, "2026-") {
		t.Errorf("expected RFC3339 timestamp string, got %s", tsJSON)
	}

	// Duration
	durJSON, err := mb.BuildMessageMockJSON("google.protobuf.Duration")
	if err != nil || !json.Valid([]byte(durJSON)) {
		t.Fatalf("failed to synthesize Duration: %v", err)
	}
	if !strings.Contains(durJSON, "10s") {
		t.Errorf("expected duration string, got %s", durJSON)
	}

	// Empty
	emptyJSON, err := mb.BuildMessageMockJSON("google.protobuf.Empty")
	if err != nil || !json.Valid([]byte(emptyJSON)) {
		t.Fatalf("failed to synthesize Empty: %v", err)
	}

	// Struct
	structJSON, err := mb.BuildMessageMockJSON("google.protobuf.Struct")
	if err != nil || !json.Valid([]byte(structJSON)) {
		t.Fatalf("failed to synthesize Struct: %v", err)
	}
}

func TestMockBuilder_CyclicReference(t *testing.T) {
	cyclicProto := `
syntax = "proto3";
package test.cyclic;

message TreeNode {
  string val = 1;
  TreeNode left = 2;
  TreeNode right = 3;
}
`
	pf, err := ParseProtoString(cyclicProto, "cyclic.proto")
	if err != nil {
		t.Fatalf("parse cyclic proto: %v", err)
	}

	ws := NewProtoWorkspace(".")
	ws.AddFile(pf)

	mb := NewMockBuilder(ws)

	// Must not infinite loop or panic!
	jsonStr, err := mb.BuildMessageMockJSON("TreeNode")
	if err != nil {
		t.Fatalf("cyclic mock failed: %v", err)
	}
	if !json.Valid([]byte(jsonStr)) {
		t.Fatalf("invalid json for cyclic message: %s", jsonStr)
	}
}

func TestMockBuilder_RPCRequestMock(t *testing.T) {
	pf, err := ParseProtoString(sampleProto3, "test.proto")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	ws := NewProtoWorkspace(".")
	ws.AddFile(pf)

	mb := NewMockBuilder(ws)

	svc := pf.Services[0]
	rpc := &svc.Methods[0] // GetUser(GetUserRequest)

	mockStr, err := mb.BuildRPCRequestMock(rpc)
	if err != nil {
		t.Fatalf("BuildRPCRequestMock error: %v", err)
	}
	if !strings.Contains(mockStr, "usr_1001") {
		t.Errorf("expected mock to contain 'usr_1001', got %s", mockStr)
	}

	// Empty request
	emptyRPC := &RPCMethod{
		Name:        "TestEmpty",
		RequestType: "google.protobuf.Empty",
	}
	emptyMock, err := mb.BuildRPCRequestMock(emptyRPC)
	if err != nil {
		t.Fatalf("empty rpc mock error: %v", err)
	}
	if !strings.Contains(emptyMock, "{") {
		t.Errorf("expected empty JSON object, got %s", emptyMock)
	}
}

func TestCodegen_CommandFormatting(t *testing.T) {
	tests := []struct {
		lang       string
		grpc       bool
		expectArg  string
		expectGRPC string
	}{
		{
			lang:       LanguageGo,
			grpc:       true,
			expectArg:  "--go_out=./gen",
			expectGRPC: "--go-grpc_out=./gen",
		},
		{
			lang:       LanguagePython,
			grpc:       true,
			expectArg:  "--python_out=./gen",
			expectGRPC: "--grpc_python_out=./gen",
		},
		{
			lang:       LanguageRust,
			grpc:       true,
			expectArg:  "--rust_out=./gen",
			expectGRPC: "--grpc-rust_out=./gen",
		},
		{
			lang:       LanguageTypeScript,
			grpc:       true,
			expectArg:  "--ts_proto_out=./gen",
			expectGRPC: "outputServices=grpc-js",
		},
		{
			lang:       LanguageCpp,
			grpc:       true,
			expectArg:  "--cpp_out=./gen",
			expectGRPC: "--grpc_out=./gen",
		},
	}

	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			cfg := CodegenConfig{
				Language:     tt.lang,
				ProtoFiles:   []string{"test.proto"},
				ImportPaths:  []string{"./proto"},
				OutputDir:    "./gen",
				GenerateGRPC: tt.grpc,
			}

			cmd, err := BuildCodegenCommand(cfg)
			if err != nil {
				t.Fatalf("BuildCodegenCommand failed for %s: %v", tt.lang, err)
			}

			cmdStr := cmd.CommandString()
			if !strings.Contains(cmdStr, tt.expectArg) {
				t.Errorf("%s: expected command to contain %s, got %s", tt.lang, tt.expectArg, cmdStr)
			}
			if tt.expectGRPC != "" && !strings.Contains(cmdStr, tt.expectGRPC) {
				t.Errorf("%s: expected command to contain gRPC flag %s, got %s", tt.lang, tt.expectGRPC, cmdStr)
			}
		})
	}
}

func TestCodegen_MissingCompilerFeedback(t *testing.T) {
	for _, lang := range SupportedLanguages {
		help := GetMissingCompilerHelp(lang, "missing_protoc_xyz")
		if !strings.Contains(help, "missing_protoc_xyz") {
			t.Errorf("expected help to mention executable name, got %s", help)
		}
		if !strings.Contains(help, "Protocol Buffers compiler") {
			t.Errorf("expected general install instructions, got %s", help)
		}
	}

	// Test ExecuteCodegen with a guaranteed missing compiler
	cfg := CodegenConfig{
		Language:       LanguageGo,
		ProtoFiles:     []string{"test.proto"},
		CustomCompiler: "non_existent_compiler_binary_xyz_123",
	}

	res, err := ExecuteCodegen(context.Background(), cfg)
	if err == nil {
		t.Fatalf("expected error for non-existent compiler, got nil")
	}
	if res == nil || res.ExitCode != 127 || res.HelpGuide == "" {
		t.Fatalf("expected exit code 127 and help guide in result: %+v", res)
	}
}

func TestWorkspace_Discovery(t *testing.T) {
	tmpDir := t.TempDir()

	file1 := filepath.Join(tmpDir, "svc1.proto")
	file2 := filepath.Join(tmpDir, "svc2.proto")

	protoContent1 := `syntax = "proto3"; package pkg1; service Svc1 { rpc Call1(Msg1) returns (Msg1); } message Msg1 { string id = 1; }`
	protoContent2 := `syntax = "proto3"; package pkg2; service Svc2 { rpc Call2(Msg2) returns (Msg2); } message Msg2 { int32 count = 1; }`

	if err := os.WriteFile(file1, []byte(protoContent1), 0644); err != nil {
		t.Fatalf("write file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte(protoContent2), 0644); err != nil {
		t.Fatalf("write file2: %v", err)
	}

	discovered, err := DiscoverProtoFiles(tmpDir)
	if err != nil {
		t.Fatalf("DiscoverProtoFiles error: %v", err)
	}
	if len(discovered) != 2 {
		t.Fatalf("expected 2 discovered files, got %d", len(discovered))
	}

	ws, err := LoadWorkspace(tmpDir)
	if err != nil {
		t.Fatalf("LoadWorkspace error: %v", err)
	}
	if len(ws.AllServices()) != 2 {
		t.Errorf("expected 2 services in workspace, got %d", len(ws.AllServices()))
	}
	if ws.FindMessage("Msg1") == nil || ws.FindMessage("pkg1.Msg1") == nil {
		t.Errorf("could not find Msg1 in workspace")
	}
	if ws.FindService("Svc2") == nil || ws.FindService("pkg2.Svc2") == nil {
		t.Errorf("could not find Svc2 in workspace")
	}
}
