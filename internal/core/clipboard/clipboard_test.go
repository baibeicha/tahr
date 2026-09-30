package clipboard

import "testing"

func TestClipboard_ReadWrite(t *testing.T) {
	testText := "Tahr IDE: High-Performance Terminal & GUI IDE"
	err := Write(testText)
	if err != nil {
		t.Logf("Write returned error: %v (falling back to memory)", err)
	}

	read, err := Read()
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if read != testText {
		t.Fatalf("expected %q, got %q", testText, read)
	}
}
