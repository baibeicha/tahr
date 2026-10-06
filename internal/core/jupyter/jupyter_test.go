package jupyter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseNotebook(t *testing.T) {
	rawJSON := `{
 "cells": [
  {
   "cell_type": "markdown",
   "metadata": {},
   "source": [
    "# Jupyter Notebook Engine\n",
    "This is a test markdown cell."
   ]
  },
  {
   "cell_type": "code",
   "execution_count": 1,
   "id": "cell-abc1",
   "metadata": {},
   "outputs": [
    {
     "name": "stdout",
     "output_type": "stream",
     "text": [
      "Hello Tahr\n"
     ]
    }
   ],
   "source": [
    "print('Hello Tahr')"
   ]
  },
  {
   "cell_type": "code",
   "execution_count": 2,
   "id": "cell-abc2",
   "metadata": {},
   "outputs": [
    {
     "output_type": "execute_result",
     "execution_count": 2,
     "data": {
      "text/plain": "42"
     }
    }
   ],
   "source": "40 + 2"
  }
 ],
 "metadata": {
  "language_info": {
   "name": "python"
  }
 },
 "nbformat": 4,
 "nbformat_minor": 5
}`

	nb, err := ParseNotebook([]byte(rawJSON))
	if err != nil {
		t.Fatalf("ParseNotebook returned error: %v", err)
	}

	if nb.Nbformat != 4 || nb.NbformatMinor != 5 {
		t.Errorf("Expected nbformat 4.5, got %d.%d", nb.Nbformat, nb.NbformatMinor)
	}

	if len(nb.Cells) != 3 {
		t.Fatalf("Expected 3 cells, got %d", len(nb.Cells))
	}

	// Cell 0: markdown
	c0 := nb.Cells[0]
	if c0.CellType != CellTypeMarkdown {
		t.Errorf("Cell 0: expected markdown, got %s", c0.CellType)
	}
	if !strings.Contains(c0.GetSource(), "Jupyter Notebook Engine") {
		t.Errorf("Cell 0 source missing expected content: %s", c0.GetSource())
	}

	// Cell 1: code
	c1 := nb.Cells[1]
	if c1.CellType != CellTypeCode {
		t.Errorf("Cell 1: expected code, got %s", c1.CellType)
	}
	if c1.ExecutionCount == nil || *c1.ExecutionCount != 1 {
		t.Errorf("Cell 1: expected execution count 1, got %v", c1.ExecutionCount)
	}
	if len(c1.Outputs) != 1 {
		t.Fatalf("Cell 1: expected 1 output, got %d", len(c1.Outputs))
	}
	if c1.Outputs[0].OutputType != OutputTypeStream {
		t.Errorf("Cell 1 output: expected stream, got %s", c1.Outputs[0].OutputType)
	}
	if c1.Outputs[0].TextContent() != "Hello Tahr\n" {
		t.Errorf("Cell 1 output text mismatch: %q", c1.Outputs[0].TextContent())
	}

	// Cell 2: code with single string source and execute_result
	c2 := nb.Cells[2]
	if c2.GetSource() != "40 + 2" {
		t.Errorf("Cell 2 source: expected '40 + 2', got %q", c2.GetSource())
	}
	if len(c2.Outputs) != 1 {
		t.Fatalf("Cell 2: expected 1 output, got %d", len(c2.Outputs))
	}
	if c2.Outputs[0].OutputType != OutputTypeExecuteResult {
		t.Errorf("Cell 2 output: expected execute_result, got %s", c2.Outputs[0].OutputType)
	}
	if c2.Outputs[0].TextContent() != "42" {
		t.Errorf("Cell 2 output text mismatch: %q", c2.Outputs[0].TextContent())
	}
}

func TestNotebookSaveAndReload(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test_notebook.ipynb")

	nb := NewNotebook()
	mdCell := nb.AddCell(CellTypeMarkdown, "## Header Title\nIntroductory text")
	if mdCell == nil {
		t.Fatal("AddCell returned nil")
	}

	codeCell := nb.AddCell(CellTypeCode, "x = 10\nprint(x)")
	execCount := 4
	codeCell.ExecutionCount = &execCount
	codeCell.AddOutput(Output{
		OutputType: OutputTypeStream,
		Name:       "stdout",
		Text:       []string{"10\n"},
	})
	codeCell.AddOutput(Output{
		OutputType:     OutputTypeExecuteResult,
		ExecutionCount: &execCount,
		Data: map[string]interface{}{
			"text/plain": "10",
		},
	})
	codeCell.AddOutput(Output{
		OutputType: OutputTypeError,
		EName:      "CustomError",
		EValue:     "something failed",
		Traceback:  []string{"Traceback (most recent call last):", "CustomError: something failed"},
	})

	if err := nb.Save(filePath); err != nil {
		t.Fatalf("Save notebook failed: %v", err)
	}

	// Reload notebook from file
	reloaded, err := LoadNotebook(filePath)
	if err != nil {
		t.Fatalf("LoadNotebook failed: %v", err)
	}

	if len(reloaded.Cells) != 2 {
		t.Fatalf("Expected 2 cells after reload, got %d", len(reloaded.Cells))
	}

	loadedCode := reloaded.Cells[1]
	if loadedCode.ExecutionCount == nil || *loadedCode.ExecutionCount != 4 {
		t.Errorf("Expected execution count 4, got %v", loadedCode.ExecutionCount)
	}

	if len(loadedCode.Outputs) != 3 {
		t.Fatalf("Expected 3 outputs, got %d", len(loadedCode.Outputs))
	}

	// Verify stream
	if loadedCode.Outputs[0].OutputType != OutputTypeStream || loadedCode.Outputs[0].Name != "stdout" {
		t.Errorf("Output 0 mismatch: %+v", loadedCode.Outputs[0])
	}
	// Verify execute_result
	if loadedCode.Outputs[1].OutputType != OutputTypeExecuteResult || loadedCode.Outputs[1].TextContent() != "10" {
		t.Errorf("Output 1 mismatch: %+v", loadedCode.Outputs[1])
	}
	// Verify error
	if loadedCode.Outputs[2].OutputType != OutputTypeError || loadedCode.Outputs[2].EName != "CustomError" {
		t.Errorf("Output 2 mismatch: %+v", loadedCode.Outputs[2])
	}
}

func TestLocalExecutionFallback(t *testing.T) {
	// Auto-detect python interpreter
	pyPath, err := DetectPython("")
	if err != nil {
		t.Skipf("Skipping local execution test: %v", err)
	}
	if pyPath == "" {
		t.Skip("Python not found on machine")
	}

	client := NewClient(ClientConfig{
		ServerURL: "", // forces local execution
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 1. Test stdout print
	cellPrint := &Cell{
		CellType: CellTypeCode,
	}
	cellPrint.SetSource("print('hello fallback runner')")

	if err := client.ExecuteCellLocal(ctx, cellPrint); err != nil {
		t.Fatalf("ExecuteCellLocal print failed: %v", err)
	}

	if cellPrint.ExecutionCount == nil || *cellPrint.ExecutionCount != 1 {
		t.Errorf("Expected execution count 1, got %v", cellPrint.ExecutionCount)
	}
	if len(cellPrint.Outputs) == 0 {
		t.Fatalf("Expected stdout output, got none")
	}
	if cellPrint.Outputs[0].OutputType != OutputTypeStream || !strings.Contains(cellPrint.Outputs[0].TextContent(), "hello fallback runner") {
		t.Errorf("Unexpected print output: %+v", cellPrint.Outputs[0])
	}

	// 2. Test expression evaluation result
	cellExpr := &Cell{
		CellType: CellTypeCode,
	}
	cellExpr.SetSource("a = 21\na * 2")

	if err := client.ExecuteCellLocal(ctx, cellExpr); err != nil {
		t.Fatalf("ExecuteCellLocal expr failed: %v", err)
	}

	if len(cellExpr.Outputs) == 0 {
		t.Fatalf("Expected execute_result output, got none")
	}
	foundResult := false
	for _, out := range cellExpr.Outputs {
		if out.OutputType == OutputTypeExecuteResult && strings.TrimSpace(out.TextContent()) == "42" {
			foundResult = true
			break
		}
	}
	if !foundResult {
		t.Errorf("Expected execute_result with '42', got outputs: %+v", cellExpr.Outputs)
	}

	// 3. Test exception traceback capture
	cellErr := &Cell{
		CellType: CellTypeCode,
	}
	cellErr.SetSource("1 / 0")

	if err := client.ExecuteCellLocal(ctx, cellErr); err != nil {
		t.Fatalf("ExecuteCellLocal exception unexpectedly failed: %v", err)
	}

	if len(cellErr.Outputs) == 0 {
		t.Fatalf("Expected error output for 1 / 0, got none")
	}
	lastOut := cellErr.Outputs[len(cellErr.Outputs)-1]
	if lastOut.OutputType != OutputTypeError {
		t.Errorf("Expected OutputTypeError, got %s", lastOut.OutputType)
	}
	if lastOut.EName != "ZeroDivisionError" {
		t.Errorf("Expected ZeroDivisionError, got %s", lastOut.EName)
	}
	if !strings.Contains(lastOut.TextContent(), "ZeroDivisionError") {
		t.Errorf("Traceback missing ZeroDivisionError: %s", lastOut.TextContent())
	}
}

func TestJupyterServerRESTEndpoints(t *testing.T) {
	// Setup mock Jupyter REST API server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/status":
			status := ServerStatus{
				Started:      "2026-10-06T10:00:00Z",
				LastActivity: "2026-10-06T10:05:00Z",
				Kernels:      2,
				Connections:  1,
				Version:      "7.0.0",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(status)

		case r.Method == http.MethodPost && r.URL.Path == "/api/kernels":
			kernel := KernelInfo{
				ID:             "mock-kernel-42",
				Name:           "python3",
				LastActivity:   "2026-10-06T10:05:00Z",
				ExecutionState: "starting",
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(kernel)

		case r.Method == http.MethodGet && r.URL.Path == "/api/kernels":
			kernels := []KernelInfo{
				{
					ID:             "mock-kernel-42",
					Name:           "python3",
					ExecutionState: "idle",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(kernels)

		case r.Method == http.MethodDelete && r.URL.Path == "/api/kernels/mock-kernel-42":
			w.WriteHeader(http.StatusNoContent)

		default:
			http.NotFound(w, r)
		}
	}))
	defer mockServer.Close()

	client := NewClient(ClientConfig{
		ServerURL: mockServer.URL,
		Token:     "test-token",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. Test GET /api/status
	status, err := client.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.Kernels != 2 || status.Version != "7.0.0" {
		t.Errorf("Unexpected status: %+v", status)
	}

	// 2. Test POST /api/kernels
	kernel, err := client.StartKernel(ctx, "python3")
	if err != nil {
		t.Fatalf("StartKernel failed: %v", err)
	}
	if kernel.ID != "mock-kernel-42" {
		t.Errorf("Unexpected kernel ID: %s", kernel.ID)
	}

	// 3. Test GET /api/kernels
	kernels, err := client.ListKernels(ctx)
	if err != nil {
		t.Fatalf("ListKernels failed: %v", err)
	}
	if len(kernels) != 1 || kernels[0].ID != "mock-kernel-42" {
		t.Errorf("Unexpected kernels list: %+v", kernels)
	}

	// 4. Test DELETE /api/kernels/<id>
	if err := client.DeleteKernel(ctx, kernel.ID); err != nil {
		t.Fatalf("DeleteKernel failed: %v", err)
	}
}

func TestNotebookCellManipulation(t *testing.T) {
	nb := NewNotebook()
	c1 := nb.AddCell(CellTypeCode, "print(1)")
	c2 := nb.AddCell(CellTypeCode, "print(2)")
	c3 := nb.AddCell(CellTypeMarkdown, "# Note")

	if len(nb.Cells) != 3 {
		t.Fatalf("Expected 3 cells, got %d", len(nb.Cells))
	}

	// Insert in the middle
	inserted := nb.InsertCell(1, CellTypeCode, "print('middle')")
	if len(nb.Cells) != 4 {
		t.Fatalf("Expected 4 cells after insert, got %d", len(nb.Cells))
	}
	if nb.Cells[1].GetSource() != "print('middle')" {
		t.Errorf("Inserted cell source mismatch: %s", nb.Cells[1].GetSource())
	}
	_ = inserted
	_ = c1
	_ = c2
	_ = c3

	// Clear outputs
	cnt := 10
	nb.Cells[0].ExecutionCount = &cnt
	nb.Cells[0].AddOutput(Output{OutputType: OutputTypeStream, Name: "stdout", Text: []string{"test"}})
	nb.ClearCellOutputs(0)
	if len(nb.Cells[0].Outputs) != 0 || nb.Cells[0].ExecutionCount != nil {
		t.Errorf("Outputs not cleared on cell 0")
	}

	// Delete cell
	ok := nb.DeleteCell(1)
	if !ok || len(nb.Cells) != 3 {
		t.Errorf("DeleteCell failed or wrong cell count: %d", len(nb.Cells))
	}
	if nb.Cells[1].GetSource() != "print(2)" {
		t.Errorf("Cell 1 should now be print(2), got %s", nb.Cells[1].GetSource())
	}
}
