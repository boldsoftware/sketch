package bubbletea

import (
	"strings"
	"testing"
)

func TestNewMessagesComponent(t *testing.T) {
	comp := NewMessagesComponent()
	if comp == nil {
		t.Fatal("NewMessagesComponent returned nil")
	}

	// Test that it implements UIComponent interface
	if _, ok := comp.(UIComponent); !ok {
		t.Fatal("MessagesComponent does not implement UIComponent interface")
	}
}

func TestNewInputComponent(t *testing.T) {
	comp := NewInputComponent()
	if comp == nil {
		t.Fatal("NewInputComponent returned nil")
	}

	// Test that it implements UIComponent interface
	if _, ok := comp.(UIComponent); !ok {
		t.Fatal("InputComponent does not implement UIComponent interface")
	}
}

func TestNewStatusComponent(t *testing.T) {
	comp := NewStatusComponent()
	if comp == nil {
		t.Fatal("NewStatusComponent returned nil")
	}

	// Test that it implements UIComponent interface
	if _, ok := comp.(UIComponent); !ok {
		t.Fatal("StatusComponent does not implement UIComponent interface")
	}
}

func TestMessageWrapping(t *testing.T) {
	comp := NewMessagesComponent().(*MessagesComponent)
	comp.width = 20

	text := "This is a very long message that should be wrapped"
	wrapped := comp.wrapText(text, 20)

	if wrapped == "" {
		t.Fatal("wrapText returned empty string")
	}

	// Check that no line is longer than the width
	lines := strings.Split(wrapped, "\n")
	for _, line := range lines {
		if len(line) > 20 {
			t.Fatalf("Line too long: %q (length: %d)", line, len(line))
		}
	}
}
