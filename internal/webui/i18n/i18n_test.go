package i18n

import "testing"

func TestEnglishFallback(t *testing.T) {
	if got := T("en", "nav.dashboard"); got != "Dashboard" {
		t.Fatalf("en: %q", got)
	}
	if got := T("ru", "nav.dashboard"); got != "Панель" {
		t.Fatalf("ru: %q", got)
	}
	if got := T("ru", "does.not.exist"); got == "" {
		t.Fatal("missing key should still return something")
	}
	if T("de", "nav.dashboard") != T("en", "nav.dashboard") {
		t.Fatal("unknown locale must fall back to English")
	}
	if Normalize("ru-RU") != "ru" {
		t.Fatal("ru-RU")
	}
	if FromAcceptLanguage("ru-RU,ru;q=0.9,en;q=0.8") != "ru" {
		t.Fatal("accept-language")
	}
}

func TestCatalogParity(t *testing.T) {
	for k := range catalogs["en"] {
		if _, ok := catalogs["ru"][k]; !ok {
			t.Errorf("ru missing %s", k)
		}
	}
	for k := range catalogs["ru"] {
		if _, ok := catalogs["en"][k]; !ok {
			t.Errorf("en missing %s", k)
		}
	}
}
