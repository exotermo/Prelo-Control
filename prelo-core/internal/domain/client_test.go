package domain

import "testing"

func TestNormalizePhone(t *testing.T) {
	ok := map[string]string{
		"(41) 98445-0529":    "+5541984450529",
		"41 8445-0529":       "+554184450529",
		"041 98445-0529":     "+5541984450529",
		"+55 41 98445-0529":  "+5541984450529",
		"5541984450529":      "+5541984450529",
		"0055 41 98445 0529": "+5541984450529",
		"+1 (415) 555-0100":  "+14155550100",
	}
	for in, want := range ok {
		got, err := NormalizePhone(in)
		if err != nil || got != want {
			t.Errorf("NormalizePhone(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "123", "abc", "+0123456", "984450529"} {
		if got, err := NormalizePhone(in); err == nil {
			t.Errorf("NormalizePhone(%q) = %q, want error", in, got)
		}
	}
}

func TestNewClientContactNormalizes(t *testing.T) {
	id := NewClientID()
	c, err := NewClientContact(id, ContactKindEmail, "  Fulano@Exemplo.COM ", false)
	if err != nil || c.Value != "fulano@exemplo.com" {
		t.Fatalf("email = %q, %v", c.Value, err)
	}
	if _, err := NewClientContact(id, ContactKindEmail, "Fulano <f@x.com>", false); err == nil {
		t.Fatal("display-name e-mail should be rejected")
	}
	if _, err := NewClientContact(id, "FAX", "1", false); err == nil {
		t.Fatal("unknown kind should be rejected")
	}
}

func TestNewClientValidation(t *testing.T) {
	c, err := NewClient(ClientFields{Name: "  Padaria  "}, ClientSourceManual, "u")
	if err != nil || c.Name != "Padaria" || c.Status != ClientStatusActive || c.Stage != ClientStageNew {
		t.Fatalf("defaults wrong: %+v %v", c, err)
	}
	bad := "ftp://x"
	if _, err := NewClient(ClientFields{Name: "x", Website: &bad}, ClientSourceManual, "u"); err == nil {
		t.Fatal("non-http website should be rejected")
	}
	if _, err := NewClient(ClientFields{Name: " "}, ClientSourceManual, "u"); err == nil {
		t.Fatal("blank name should be rejected")
	}
	if _, err := NewClient(ClientFields{Name: "x", Status: "VIP"}, ClientSourceManual, "u"); err == nil {
		t.Fatal("unknown status should be rejected")
	}
}
