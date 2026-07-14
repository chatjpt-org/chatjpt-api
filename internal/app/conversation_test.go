package app

import "testing"

func TestConversationTitle(t *testing.T) {
	title, err := conversationTitle("  Projeto local  ")
	if err != nil {
		t.Fatalf("conversationTitle() error = %v", err)
	}
	if title != "Projeto local" {
		t.Errorf("conversationTitle() = %q, want Projeto local", title)
	}

	title, err = conversationTitle("   ")
	if err != nil {
		t.Fatalf("conversationTitle(empty) error = %v", err)
	}
	if title != "Nova conversa" {
		t.Errorf("conversationTitle(empty) = %q, want Nova conversa", title)
	}
}

func TestIsUUID(t *testing.T) {
	if !isUUID("6dca2b6c-6061-4e66-a1bc-946fca5e9dfd") {
		t.Error("isUUID() = false, want true")
	}
	if isUUID("not-a-uuid") {
		t.Error("isUUID() = true, want false")
	}
}
