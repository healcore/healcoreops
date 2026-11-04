package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/healcore/healcoreops/internal/i18n"
	"github.com/healcore/healcoreops/internal/policy"
)

// UpdateRepo — репозиторий GitHub, в релизах которого выходят новые версии.
const UpdateRepo = "healcore/healcoreops"

// ReleasesPage — страница релизов (её открывает кнопка «Обновить вручную»).
const ReleasesPage = "https://github.com/" + UpdateRepo + "/releases"

// UpdateInfo — сведения о доступной версии. Программа только проверяет и
// уведомляет; скачивание и установка — вручную со страницы релиза.
type UpdateInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	Notes     string `json:"notes"`
	Page      string `json:"page"`
}

type ghRelease struct {
	Tag     string `json:"tag_name"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	Draft   bool   `json:"draft"`
}

func parseVer(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	var out [3]int
	for i, part := range strings.SplitN(v, ".", 3) {
		if i > 2 {
			break
		}
		n, _ := strconv.Atoi(strings.TrimFunc(part, func(r rune) bool { return r < '0' || r > '9' }))
		out[i] = n
	}
	return out
}

func newer(latest, current string) bool {
	a, b := parseVer(latest), parseVer(current)
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

var httpClient = &http.Client{Timeout: 20 * time.Second}

// CheckUpdate запрашивает последний релиз на GitHub (только чтение сведений).
func (c *Core) CheckUpdate(ctx context.Context) (UpdateInfo, error) {
	info := UpdateInfo{Current: Version, Page: ReleasesPage}
	if policy.Get().DisableUpdateCheck {
		return info, errors.New(i18n.T("проверка обновлений отключена администратором"))
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+UpdateRepo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "HealCoreOps/"+Version)
	resp, err := httpClient.Do(req)
	if err != nil {
		return info, errors.New(i18n.T("нет связи с GitHub"))
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return info, errors.New(i18n.T("релизы пока не опубликованы"))
	}
	if resp.StatusCode != http.StatusOK {
		return info, fmt.Errorf("GitHub: %s", resp.Status)
	}
	var rel ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return info, err
	}
	info.Latest = strings.TrimPrefix(rel.Tag, "v")
	info.Available = !rel.Draft && newer(rel.Tag, Version)
	if rel.HTMLURL != "" {
		info.Page = rel.HTMLURL
	}
	notes := strings.TrimSpace(rel.Body)
	if r := []rune(notes); len(r) > 1200 {
		notes = string(r[:1200]) + "…"
	}
	info.Notes = notes
	return info, nil
}

// autoUpdateCheck раз в сутки проверяет обновления и уведомляет о новой версии.
// Ничего не скачивает и не устанавливает.
func (c *Core) autoUpdateCheck() {
	time.Sleep(20 * time.Second)
	for {
		cfg := c.Config()
		if cfg.AutoUpdateCheck && !policy.Get().DisableUpdateCheck {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			info, err := c.CheckUpdate(ctx)
			cancel()
			if err == nil && info.Available && info.Latest != cfg.SkipVersion {
				c.mu.Lock()
				c.update = &info
				c.mu.Unlock()
				c.Log(LvlInfo, i18n.T("Доступна новая версия HealCoreOps %s"), info.Latest)
				if c.OnNotify != nil && cfg.Notify {
					c.OnNotify("HealCoreOps", fmt.Sprintf(i18n.T("Доступна новая версия HealCoreOps %s"), info.Latest))
				}
			}
		}
		time.Sleep(24 * time.Hour)
	}
}

// PendingUpdate — найденное фоновой проверкой обновление (или nil).
func (c *Core) PendingUpdate() *UpdateInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.update
}
