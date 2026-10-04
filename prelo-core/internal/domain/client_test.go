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

func TestCanonicalWhatsAppAddress(t *testing.T) {
	ok := map[string]string{
		"26668123456789@lid":              "26668123456789@lid",
		" 26668123456789@LID ":            "26668123456789@lid",
		"554184450529@s.whatsapp.net":     "+554184450529",
		"5541984450529:12@s.whatsapp.net": "+5541984450529",
		"5541984450529@c.us":              "+5541984450529",
		"(41) 98445-0529":                 "+5541984450529",
	}
	for in, want := range ok {
		if got, err := CanonicalWhatsAppAddress(in); err != nil || got != want {
			t.Errorf("CanonicalWhatsAppAddress(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"abc@lid", "12@lid", "", "grupo@g.us"} {
		if got, err := CanonicalWhatsAppAddress(in); err == nil {
			t.Errorf("CanonicalWhatsAppAddress(%q) = %q, want error", in, got)
		}
	}
}

func TestContactMatchKeysBrazilianNinthDigit(t *testing.T) {
	cases := map[string][]string{
		"+5541984450529":     {"+5541984450529", "+554184450529"},
		"+554184450529":      {"+554184450529", "+5541984450529"},
		"+554133334444":      {"+554133334444"}, // landline: no 9 variant
		"+14155550100":       {"+14155550100"},
		"26668123456789@lid": {"26668123456789@lid"},
	}
	for in, want := range cases {
		got := ContactMatchKeys(in)
		if len(got) != len(want) {
			t.Fatalf("ContactMatchKeys(%q) = %v, want %v", in, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("ContactMatchKeys(%q) = %v, want %v", in, got, want)
			}
		}
	}
}
