// Package i18n — перевод сообщений бэкенда (русский — язык-ключ, английский — словарь).
package i18n

import (
	"os"
	"strings"
	"sync/atomic"
)

var cur atomic.Value

func init() { cur.Store("ru") }

// Set выбирает язык: "ru" или "en".
func Set(l string) {
	if l != "en" {
		l = "ru"
	}
	cur.Store(l)
}

// Get возвращает текущий язык.
func Get() string { return cur.Load().(string) }

// T переводит строку (ключ — русский текст).
func T(ru string) string {
	if Get() == "en" {
		if e, ok := en[ru]; ok {
			return e
		}
	}
	return ru
}

// Pick выбирает вариант текста по текущему языку.
func Pick(ru, en string) string {
	if Get() == "en" {
		return en
	}
	return ru
}

// SystemLang определяет язык системы: ru для русской локали, иначе en.
func SystemLang() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := strings.ToLower(os.Getenv(k)); v != "" {
			if strings.HasPrefix(v, "ru") {
				return "ru"
			}
			return "en"
		}
	}
	return systemLang()
}
