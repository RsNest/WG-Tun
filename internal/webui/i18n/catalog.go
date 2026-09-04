package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed en.json ru.json
var files embed.FS

type catalog map[string]string

var catalogs = map[string]catalog{}

func init() {
	catalogs["en"] = mustLoad("en.json")
	catalogs["ru"] = mustLoad("ru.json")
}

func mustLoad(name string) catalog {
	b, err := files.ReadFile(name)
	if err != nil {
		panic(err)
	}
	var raw any
	if err := json.Unmarshal(b, &raw); err != nil {
		panic(name + ": " + err.Error())
	}
	out := catalog{}
	flatten("", raw, out)
	return out
}

func flatten(prefix string, v any, out catalog) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			flatten(key, child, out)
		}
	case string:
		out[prefix] = t
	}
}

func Normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "ru" || strings.HasPrefix(s, "ru-") || strings.HasPrefix(s, "ru_") {
		return "ru"
	}
	return "en"
}

func FromAcceptLanguage(h string) string {
	h = strings.ToLower(h)
	for _, part := range strings.Split(h, ",") {
		tag := strings.TrimSpace(strings.Split(part, ";")[0])
		if tag == "ru" || strings.HasPrefix(tag, "ru-") {
			return "ru"
		}
		if tag == "en" || strings.HasPrefix(tag, "en-") {
			return "en"
		}
	}
	return "en"
}

func T(locale, key string) string {
	if key == "" {
		return ""
	}
	loc := Normalize(locale)
	if v, ok := catalogs[loc][key]; ok && v != "" {
		return v
	}
	if v, ok := catalogs["en"][key]; ok && v != "" {
		return v
	}
	return key
}

func Format(locale, key string, args ...any) string {
	s := T(locale, key)
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

func Translator(locale string) func(string) string {
	loc := Normalize(locale)
	return func(key string) string { return T(loc, key) }
}
