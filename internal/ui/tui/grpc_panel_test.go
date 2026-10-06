package tui

import (
	"strings"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/grpcproto"
	"tahr/internal/ui"
)

const testProto = `
syntax = "proto3";
package test.account;

message LoginRequest {
  string username = 1;
  string password = 2;
}

message LoginResponse {
  string token = 1;
  int64 expires_at = 2;
}

message GetProfileRequest {
  string user_id = 1;
}

message Profile {
  string user_id = 1;
  string display_name = 2;
}

service AuthService {
  rpc Login (LoginRequest) returns (LoginResponse);
  rpc GetProfile (GetProfileRequest) returns (Profile);
}
`

func setupTestPanel(t *testing.T) *GRPCPanel {
	pf, err := grpcproto.ParseProtoString(testProto, "auth.proto")
	if err != nil {
		t.Fatalf("failed to parse test proto: %v", err)
	}

	ws := grpcproto.NewProtoWorkspace(".")
	ws.AddFile(pf)

	theme := ui.CatppuccinMocha()
	panel := NewGRPCPanel(ws, &theme)
	return panel
}

func TestGRPCPanel_InitAndWorkspaceBinding(t *testing.T) {
	panel := setupTestPanel(t)

	if !panel.Open {
		t.Fatalf("expected panel to be open initially")
	}

	if len(panel.TreeItems) != 3 { // 1 service + 2 RPCs
		t.Fatalf("expected 3 tree items, got %d", len(panel.TreeItems))
	}

	// Verify first RPC is auto-selected
	if panel.ActiveRPC == nil || panel.ActiveRPC.Name != "Login" {
		t.Fatalf("expected ActiveRPC 'Login', got %+v", panel.ActiveRPC)
	}

	// Verify JSON mock template is populated
	if !strings.Contains(panel.PayloadText, "username") {
		t.Errorf("expected payload to contain 'username', got:\n%s", panel.PayloadText)
	}
}

func TestGRPCPanel_TreeNavigationAndRPCSelection(t *testing.T) {
	panel := setupTestPanel(t)
	panel.FocusArea = FocusTree
	panel.SelectedTreeIdx = 0 // Service header

	// Navigate down to first RPC (Login)
	panel.HandleKey(input.Key{Type: input.KeyDown})
	if panel.SelectedTreeIdx != 1 {
		t.Errorf("expected SelectedTreeIdx 1, got %d", panel.SelectedTreeIdx)
	}

	// Navigate down to second RPC (GetProfile)
	panel.HandleKey(input.Key{Type: input.KeyDown})
	if panel.SelectedTreeIdx != 2 {
		t.Errorf("expected SelectedTreeIdx 2, got %d", panel.SelectedTreeIdx)
	}

	// Press Enter to select GetProfile
	panel.HandleKey(input.Key{Type: input.KeyEnter})
	if panel.ActiveRPC == nil || panel.ActiveRPC.Name != "GetProfile" {
		t.Fatalf("expected ActiveRPC 'GetProfile', got %+v", panel.ActiveRPC)
	}

	// Payload should now contain user_id from GetProfileRequest
	if !strings.Contains(panel.PayloadText, "user_id") {
		t.Errorf("expected GetProfile payload to contain 'user_id', got:\n%s", panel.PayloadText)
	}

	// Focus should shift to editor on Enter
	if panel.FocusArea != FocusEditor {
		t.Errorf("expected focus to transition to FocusEditor, got %v", panel.FocusArea)
	}
}

func TestGRPCPanel_EditorEditing(t *testing.T) {
	panel := setupTestPanel(t)
	panel.FocusArea = FocusEditor

	panel.SetPayload("{\n}")
	panel.CursorRow = 0
	panel.CursorCol = 1

	// Type 'x' after '{'
	panel.HandleKey(input.Key{Rune: 'x'})
	if panel.PayloadLines[0] != "{x" {
		t.Errorf("expected line 0 to be '{x', got %q", panel.PayloadLines[0])
	}

	// Backspace
	panel.HandleKey(input.Key{Type: input.KeyBackspace})
	if panel.PayloadLines[0] != "{" {
		t.Errorf("expected line 0 to be '{', got %q", panel.PayloadLines[0])
	}

	// Press Enter to create new line
	panel.HandleKey(input.Key{Type: input.KeyEnter})
	if len(panel.PayloadLines) != 3 {
		t.Errorf("expected 3 lines after enter, got %d", len(panel.PayloadLines))
	}
	if panel.CursorRow != 1 || panel.CursorCol != 0 {
		t.Errorf("unexpected cursor after enter: row=%d col=%d", panel.CursorRow, panel.CursorCol)
	}
}

func TestGRPCPanel_ActionButtons(t *testing.T) {
	panel := setupTestPanel(t)

	// 1. Reset button restores initial template
	original := panel.PayloadText
	panel.SetPayload(`{"custom": "modified"}`)
	panel.TriggerButton(BtnReset)
	if panel.PayloadText != original {
		t.Errorf("expected Reset to restore original mock, got %s", panel.PayloadText)
	}

	// 2. Create Mock regenerates fresh payload
	panel.SetPayload(`{}`)
	panel.TriggerButton(BtnCreateMock)
	if !strings.Contains(panel.PayloadText, "username") {
		t.Errorf("expected CreateMock to restore username payload, got %s", panel.PayloadText)
	}

	// 3. Copy Payload
	var copied string
	panel.OnCopy = func(text string) {
		copied = text
	}
	panel.TriggerButton(BtnCopyPayload)
	if copied != panel.PayloadText {
		t.Errorf("expected OnCopy to receive payload text, got %q", copied)
	}
	if !strings.Contains(panel.StatusMessage, "Copied") && !strings.Contains(panel.StatusMessage, "clipboard") {
		t.Errorf("expected status message to confirm copy, got %s", panel.StatusMessage)
	}

	// 4. Generate Code
	var generatedCmd *grpcproto.CodegenCommand
	panel.OnGenerateCode = func(cmd *grpcproto.CodegenCommand) {
		generatedCmd = cmd
	}
	panel.TriggerButton(BtnGenerateCode)
	_ = generatedCmd
	// Whether protoc is in PATH or missing, status should reflect it gracefully without crashing
	if panel.StatusMessage == "" {
		t.Errorf("expected status message after Generate Code, got empty")
	}

	// 5. Language cycling
	initLang := panel.SelectedLang
	newLang := panel.CycleLanguage()
	if newLang == initLang {
		t.Errorf("expected language to change on cycle, got %s", newLang)
	}
}

func TestGRPCPanel_RenderAndButtonStyling(t *testing.T) {
	panel := setupTestPanel(t)
	buf := buffer.NewBuffer(100, 30)
	rect := buffer.NewRect(0, 0, 100, 30)

	panel.Render(buf, rect)

	// Check header
	cell0 := buf.Cell(1, 0)
	if cell0.Rune != 'g' {
		t.Errorf("expected header rune 'g', got %c", cell0.Rune)
	}

	// Verify action buttons are rendered without brackets [ ]
	// Rendered text should contain "Generate Code", NOT "[Generate Code]"
	foundGenerateCode := false
	for y := 0; y < 30; y++ {
		var rowRunes []rune
		for x := 0; x < 100; x++ {
			rowRunes = append(rowRunes, buf.Cell(x, y).Rune)
		}
		rowStr := string(rowRunes)
		if strings.Contains(rowStr, "Generate Code") {
			foundGenerateCode = true
			if strings.Contains(rowStr, "[Generate Code]") || strings.Contains(rowStr, "[ Generate Code ]") {
				t.Fatalf("constraint violation: action button rendered with brackets [ ]: %s", rowStr)
			}
			if strings.Contains(rowStr, "[Create Mock]") || strings.Contains(rowStr, "[ Create Mock ]") {
				t.Fatalf("constraint violation: Create Mock rendered with brackets [ ]: %s", rowStr)
			}
		}
	}

	if !foundGenerateCode {
		t.Fatalf("did not find 'Generate Code' button rendered in buffer")
	}

	// Verify button hit areas are populated
	if len(panel.ButtonHits) != 4 {
		t.Fatalf("expected 4 button hit targets, got %d", len(panel.ButtonHits))
	}
}

func TestGRPCPanel_MouseClick(t *testing.T) {
	panel := setupTestPanel(t)
	buf := buffer.NewBuffer(100, 30)
	rect := buffer.NewRect(0, 0, 100, 30)
	panel.Render(buf, rect)

	// Test clicking 'Create Mock' button hit area
	var mockHit *GRPCButtonHit
	for _, h := range panel.ButtonHits {
		if h.Action == BtnCreateMock {
			mockHit = &h
			break
		}
	}

	if mockHit == nil {
		t.Fatalf("missing BtnCreateMock hit region")
	}

	panel.SetPayload(`{}`)
	handled := panel.HandleClick(mockHit.X+2, mockHit.Y, rect)
	if !handled {
		t.Errorf("expected click to be handled")
	}
	if !strings.Contains(panel.PayloadText, "username") {
		t.Errorf("expected Create Mock click to repopulate mock payload, got %s", panel.PayloadText)
	}
}
