package gogen

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"tahr/internal/core/lsp"
)

func TestTagCasing(t *testing.T) {
	tests := []struct {
		input    string
		casing   TagCase
		expected string
	}{
		{"UserID", CaseSnake, "user_id"},
		{"UserID", CaseCamel, "userId"},
		{"UserID", CaseKebab, "user-id"},
		{"UserID", CaseLower, "userid"},
		{"FirstName", CaseSnake, "first_name"},
		{"FirstName", CaseCamel, "firstName"},
		{"FirstName", CaseKebab, "first-name"},
		{"FirstName", CaseLower, "firstname"},
		{"HTTPServer", CaseSnake, "http_server"},
		{"HTTPServer", CaseCamel, "httpServer"},
		{"APIKey", CaseSnake, "api_key"},
		{"APIKey", CaseCamel, "apiKey"},
		{"URL", CaseSnake, "url"},
		{"URL", CaseCamel, "url"},
		{"ID", CaseSnake, "id"},
		{"simple", CaseSnake, "simple"},
		{"simple", CaseCamel, "simple"},
		{"already_snake", CaseSnake, "already_snake"},
		{"already-kebab", CaseSnake, "already_kebab"},
	}

	for _, tt := range tests {
		got := FormatTagKey(tt.input, tt.casing)
		if got != tt.expected {
			t.Errorf("FormatTagKey(%q, %q) = %q, expected %q", tt.input, tt.casing, got, tt.expected)
		}
	}
}

func TestAddStructTags(t *testing.T) {
	src := []byte(`package sample

type User struct {
	ID        int64
	FirstName string
	Email     string
	secretKey string
}
`)

	// 1. Add JSON tags (snake_case)
	out, err := AddTagsToStruct(src, "User", []string{"json"}, CaseSnake, false)
	if err != nil {
		t.Fatalf("AddTagsToStruct error: %v", err)
	}

	outStr := string(out)
	if !strings.Contains(outStr, `ID        int64 `+"`"+`json:"id"`+"`") {
		t.Errorf("Expected ID json tag, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, `FirstName string `+"`"+`json:"first_name"`+"`") {
		t.Errorf("Expected FirstName json tag, got:\n%s", outStr)
	}
	// Unexported field secretKey should not receive serialization tag by default
	if strings.Contains(outStr, `secretKey string `+"`") {
		t.Errorf("Unexported field secretKey should not have received json tag:\n%s", outStr)
	}

	// Verify syntax is valid Go
	if _, err := parser.ParseFile(token.NewFileSet(), "out.go", out, 0); err != nil {
		t.Fatalf("Generated code is invalid Go: %v\nCode:\n%s", err, outStr)
	}

	// 2. Add DB tags on top of existing tags
	outDB, err := AddTagsToStruct(out, "User", []string{"db"}, CaseSnake, false)
	if err != nil {
		t.Fatalf("AddTagsToStruct DB error: %v", err)
	}
	outDBStr := string(outDB)
	if !strings.Contains(outDBStr, `json:"id" db:"id"`) {
		t.Errorf("Expected both json and db tags, got:\n%s", outDBStr)
	}

	// 3. Add tag with omitempty
	outOmit, err := AddTagsToStruct(src, "User", []string{"json"}, CaseCamel, true)
	if err != nil {
		t.Fatalf("AddTagsToStruct with omitempty error: %v", err)
	}
	outOmitStr := string(outOmit)
	if !strings.Contains(outOmitStr, `json:"firstName,omitempty"`) {
		t.Errorf("Expected camelCase with omitempty, got:\n%s", outOmitStr)
	}

	// 4. Remove tags
	cleaned, err := RemoveTagsFromStruct(outDB, "User", []string{"json"})
	if err != nil {
		t.Fatalf("RemoveTagsFromStruct error: %v", err)
	}
	cleanStr := string(cleaned)
	if strings.Contains(cleanStr, "json:") {
		t.Errorf("Expected json tag to be removed, got:\n%s", cleanStr)
	}
	if !strings.Contains(cleanStr, `db:"id"`) {
		t.Errorf("Expected db tag to remain, got:\n%s", cleanStr)
	}

	// Remove all tags
	cleanedAll, err := RemoveTagsFromStruct(cleaned, "User", []string{"db"})
	if err != nil {
		t.Fatalf("Remove all tags error: %v", err)
	}
	cleanedAllStr := string(cleanedAll)
	if strings.Contains(cleanedAllStr, "`") {
		t.Errorf("Expected no backticks remaining, got:\n%s", cleanedAllStr)
	}
}

func TestConstructorGeneration(t *testing.T) {
	src := []byte(`package model

type Server struct {
	Host string
	Port int
	tags []string
}
`)

	opts := DefaultConstructorOptions("Server")
	res, err := GenerateConstructor(src, "Server", opts)
	if err != nil {
		t.Fatalf("GenerateConstructor error: %v", err)
	}

	resStr := string(res)
	expectedFn := "func NewServer(host string, port int, tags []string) *Server {"
	if !strings.Contains(resStr, expectedFn) {
		t.Errorf("Expected signature %q in:\n%s", expectedFn, resStr)
	}
	if !strings.Contains(resStr, "Host: host,") || !strings.Contains(resStr, "Port: port,") {
		t.Errorf("Expected field assignments in constructor:\n%s", resStr)
	}

	// Verify syntax is valid Go
	if _, err := parser.ParseFile(token.NewFileSet(), "res.go", res, 0); err != nil {
		t.Fatalf("Generated constructor is invalid Go: %v\n%s", err, resStr)
	}
}

func TestConstructorGenericsAndKeywords(t *testing.T) {
	src := []byte(`package util

type Container[T any] struct {
	Type   string
	Select int
	Value  T
}
`)

	opts := DefaultConstructorOptions("Container")
	res, err := GenerateConstructor(src, "Container", opts)
	if err != nil {
		t.Fatalf("GenerateConstructor generic error: %v", err)
	}

	resStr := string(res)
	if !strings.Contains(resStr, "func NewContainer[T any](") {
		t.Errorf("Expected generic signature in:\n%s", resStr)
	}
	// Verify keyword sanitization: "Type" -> "typeVal", "Select" -> "selectVal"
	if !strings.Contains(resStr, "typeVal string") || !strings.Contains(resStr, "selectVal int") {
		t.Errorf("Expected keyword sanitization in params:\n%s", resStr)
	}
	if !strings.Contains(resStr, "*Container[T]") {
		t.Errorf("Expected return type *Container[T], got:\n%s", resStr)
	}

	// Verify syntax
	if _, err := parser.ParseFile(token.NewFileSet(), "res.go", res, 0); err != nil {
		t.Fatalf("Generated generic constructor is invalid Go: %v\n%s", err, resStr)
	}
}

func TestGettersSetters(t *testing.T) {
	src := []byte(`package domain

type Account struct {
	id      int64
	balance float64
	Name    string
}
`)

	info, err := FindStructByName(src, "Account")
	if err != nil {
		t.Fatalf("FindStructByName error: %v", err)
	}

	opts := DefaultGetSetOptions(info)
	res, err := GenerateGettersSetters(src, "Account", opts)
	if err != nil {
		t.Fatalf("GenerateGettersSetters error: %v", err)
	}

	resStr := string(res)

	// Unexported "id" -> getter "Id()" or "ID()", setter "SetId()"
	if !strings.Contains(resStr, "func (a *Account) Id() int64 {") {
		t.Errorf("Expected getter Id() in:\n%s", resStr)
	}
	if !strings.Contains(resStr, "func (a *Account) SetId(val int64) {") {
		t.Errorf("Expected setter SetId() in:\n%s", resStr)
	}

	// Unexported "balance" -> getter "Balance() float64", setter "SetBalance(val float64)"
	if !strings.Contains(resStr, "func (a *Account) Balance() float64 {") {
		t.Errorf("Expected getter Balance() in:\n%s", resStr)
	}
	if !strings.Contains(resStr, "func (a *Account) SetBalance(val float64) {") {
		t.Errorf("Expected setter SetBalance() in:\n%s", resStr)
	}

	// Exported "Name": getter MUST be "GetName()" to avoid collision with field "Name"
	if !strings.Contains(resStr, "func (a *Account) GetName() string {") {
		t.Errorf("Expected getter GetName() for exported field in:\n%s", resStr)
	}
	if !strings.Contains(resStr, "func (a *Account) SetName(val string) {") {
		t.Errorf("Expected setter SetName() in:\n%s", resStr)
	}

	// Verify syntax
	if _, err := parser.ParseFile(token.NewFileSet(), "res.go", res, 0); err != nil {
		t.Fatalf("Generated getters/setters is invalid Go: %v\n%s", err, resStr)
	}
}

func TestDeepCopyGeneration(t *testing.T) {
	src := []byte(`package state

type Session struct {
	ID        string
	Active    bool
	Count     int
	Tokens    []string
	Data      map[string]any
	Config    *Session
}
`)

	info, err := FindStructByName(src, "Session")
	if err != nil {
		t.Fatalf("FindStructByName error: %v", err)
	}

	// 1. Deep clone
	deepOpts := DefaultDeepCopyOptions(info)
	deepOpts.Deep = true
	resDeep, err := GenerateDeepCopy(src, "Session", deepOpts)
	if err != nil {
		t.Fatalf("GenerateDeepCopy error: %v", err)
	}

	deepStr := string(resDeep)
	if !strings.Contains(deepStr, "func (s *Session) Clone() *Session {") {
		t.Errorf("Expected Clone signature in:\n%s", deepStr)
	}
	if !strings.Contains(deepStr, "if s == nil {") {
		t.Errorf("Expected nil receiver check in:\n%s", deepStr)
	}
	if !strings.Contains(deepStr, "clone.Tokens = make([]string, len(s.Tokens))") {
		t.Errorf("Expected slice duplication in:\n%s", deepStr)
	}
	if !strings.Contains(deepStr, "clone.Data = make(map[string]any, len(s.Data))") {
		t.Errorf("Expected map duplication in:\n%s", deepStr)
	}
	if !strings.Contains(deepStr, "val := *s.Config") || !strings.Contains(deepStr, "clone.Config = &val") {
		t.Errorf("Expected pointer duplication in:\n%s", deepStr)
	}

	// Verify syntax
	if _, err := parser.ParseFile(token.NewFileSet(), "res.go", resDeep, 0); err != nil {
		t.Fatalf("Generated deep clone is invalid Go: %v\n%s", err, deepStr)
	}

	// 2. Memberwise / shallow clone
	shallowOpts := DefaultDeepCopyOptions(info)
	shallowOpts.Deep = false
	resShallow, err := GenerateDeepCopy(src, "Session", shallowOpts)
	if err != nil {
		t.Fatalf("GenerateDeepCopy shallow error: %v", err)
	}
	shallowStr := string(resShallow)
	if !strings.Contains(shallowStr, "clone := *s") {
		t.Errorf("Expected memberwise clone in shallow copy:\n%s", shallowStr)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "res.go", resShallow, 0); err != nil {
		t.Fatalf("Generated shallow clone is invalid Go: %v\n%s", err, shallowStr)
	}
}

func TestInterfaceImplementation(t *testing.T) {
	src := []byte(`package buffer

type StreamBuffer struct {
	data []byte
}

type CustomWorker interface {
	Work(job string, priority int) (bool, error)
	Status() string
}
`)

	info, err := FindStructByName(src, "StreamBuffer")
	if err != nil {
		t.Fatalf("FindStructByName error: %v", err)
	}

	// 1. Implement io.Reader
	opts := DefaultImplOptions(info)
	resReader, err := GenerateInterfaceImpl(src, "StreamBuffer", "io.Reader", opts)
	if err != nil {
		t.Fatalf("GenerateInterfaceImpl io.Reader error: %v", err)
	}
	readerStr := string(resReader)
	if !strings.Contains(readerStr, "func (s *StreamBuffer) Read(p []byte) (n int, err error) {") {
		t.Errorf("Expected Read stub in:\n%s", readerStr)
	}
	if !strings.Contains(readerStr, `panic("unimplemented")`) {
		t.Errorf("Expected panic body in:\n%s", readerStr)
	}

	// 2. Implement error
	resErr, err := GenerateInterfaceImpl(src, "StreamBuffer", "error", opts)
	if err != nil {
		t.Fatalf("GenerateInterfaceImpl error: %v", err)
	}
	errStr := string(resErr)
	if !strings.Contains(errStr, "func (s *StreamBuffer) Error() string {") {
		t.Errorf("Expected Error stub in:\n%s", errStr)
	}

	// 3. Implement fmt.Stringer
	resStringer, err := GenerateInterfaceImpl(src, "StreamBuffer", "fmt.Stringer", opts)
	if err != nil {
		t.Fatalf("GenerateInterfaceImpl fmt.Stringer error: %v", err)
	}
	stringerStr := string(resStringer)
	if !strings.Contains(stringerStr, "func (s *StreamBuffer) String() string {") {
		t.Errorf("Expected String stub in:\n%s", stringerStr)
	}

	// 4. Implement User-defined interface from source
	resCustom, err := GenerateInterfaceImpl(src, "StreamBuffer", "CustomWorker", opts)
	if err != nil {
		t.Fatalf("GenerateInterfaceImpl CustomWorker error: %v", err)
	}
	customStr := string(resCustom)
	if !strings.Contains(customStr, "Work(job string, priority int) (bool, error)") {
		t.Errorf("Expected Work stub in:\n%s", customStr)
	}
	if !strings.Contains(customStr, "Status() string") {
		t.Errorf("Expected Status stub in:\n%s", customStr)
	}

	// 5. Zero-value stub body
	optsZero := opts
	optsZero.StubBody = "zero"
	resZero, err := GenerateInterfaceImpl(src, "StreamBuffer", "CustomWorker", optsZero)
	if err != nil {
		t.Fatalf("GenerateInterfaceImpl zero error: %v", err)
	}
	zeroStr := string(resZero)
	if !strings.Contains(zeroStr, "return false, nil") {
		t.Errorf("Expected zero values return in:\n%s", zeroStr)
	}

	// Verify syntax
	if _, err := parser.ParseFile(token.NewFileSet(), "res.go", resZero, 0); err != nil {
		t.Fatalf("Generated interface stubs are invalid Go: %v\n%s", err, zeroStr)
	}
}

func TestQuickFixActionsIntegration(t *testing.T) {
	src := []byte(`package service

type Customer struct {
	ID        int64
	FullName  string
	EmailAddr string
}
`)

	uri := "file:///d:/tahr/service/customer.go"

	// Cursor is at line 2 (inside struct declaration)
	actions := GenerateQuickFixActions(uri, src, 2, 5)
	if len(actions) == 0 {
		t.Fatalf("Expected Quick Fix actions for struct at cursor, got 0")
	}

	titles := make(map[string]bool)
	for _, a := range actions {
		titles[a.Title] = true
		if a.Edit == nil || len(a.Edit.Changes[uri]) == 0 {
			t.Errorf("Action %q has no text edits", a.Title)
		}
	}

	// Verify the 5 required actions are present
	required := []string{
		"Add JSON Tags",
		"Add DB Tags",
		"Generate Constructor",
		"Generate Getters/Setters",
		"Generate Clone / DeepCopy",
	}

	for _, req := range required {
		if !titles[req] {
			t.Errorf("Expected action %q to be offered, available: %v", req, titles)
		}
	}

	// Test applying the "Add JSON Tags" action text edits
	var jsonAction *lsp.CodeAction
	for _, a := range actions {
		if a.Title == "Add JSON Tags" {
			jsonAction = &a
			break
		}
	}
	if jsonAction == nil {
		t.Fatalf("Add JSON Tags action missing")
	}

	edits := jsonAction.Edit.Changes[uri]
	modified, err := ApplyEdits(src, edits)
	if err != nil {
		t.Fatalf("Failed to apply JSON edits: %v", err)
	}

	modStr := string(modified)
	if !strings.Contains(modStr, `ID        int64 `+"`"+`json:"id"`+"`") {
		t.Errorf("Expected JSON tag on ID, got:\n%s", modStr)
	}
	if !strings.Contains(modStr, `FullName  string `+"`"+`json:"full_name"`+"`") {
		t.Errorf("Expected JSON tag on FullName, got:\n%s", modStr)
	}

	// Cursor outside struct (e.g. line 0) should return nil
	outsideActions := GenerateQuickFixActions(uri, src, 0, 0)
	if len(outsideActions) != 0 {
		t.Errorf("Expected 0 actions outside struct, got %d", len(outsideActions))
	}
}
