//go:build windows

package tweaks

// Системные твики. Набор собран по разбору популярных открытых проектов
// оптимизации Windows (исходники изучены напрямую):
//   WinUtil (Chris Titus Tech), Win11Debloat (Raphire), Optimizer (hellzerg),
//   Sophia Script (farag2). Взяты твики, которые эти проекты считают безопасными
//   или полезными; опасные (отключение Defender, UAC, обновлений, IPv6,
//   удаление Edge, svchost splitting) сознательно не включены.
//
// Каждый твик обратим: «выключить» возвращает значения Windows по умолчанию.

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/i18n"
)

// ---------- Значения реестра ----------

type val struct {
	dword *uint32
	str   *string
	bin   []byte
}

func dw(v uint32) val      { return val{dword: &v} }
func sz(v string) val      { return val{str: &v} }
func bin(b ...byte) val    { return val{bin: b} }
func none() val            { return val{} }
func (v val) isNone() bool { return v.dword == nil && v.str == nil && v.bin == nil }

// regOp — параметр реестра: значение при включённом твике (on) и по умолчанию (off).
// off без значения означает «удалить параметр».
type regOp struct {
	root    registry.Key
	path    string
	name    string
	on, off val
}

func (o regOp) set(v val) error {
	if v.isNone() {
		k, err := registry.OpenKey(o.root, o.path, registry.SET_VALUE)
		if err != nil {
			return nil // ключа нет — значит, и параметра нет
		}
		defer k.Close()
		if err := k.DeleteValue(o.name); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	k, _, err := registry.CreateKey(o.root, o.path, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	switch {
	case v.dword != nil:
		return k.SetDWordValue(o.name, *v.dword)
	case v.bin != nil:
		return k.SetBinaryValue(o.name, v.bin)
	default:
		return k.SetStringValue(o.name, *v.str)
	}
}

func (o regOp) is(v val) bool {
	k, err := registry.OpenKey(o.root, o.path, registry.QUERY_VALUE)
	if err != nil {
		return v.isNone()
	}
	defer k.Close()
	switch {
	case v.dword != nil:
		n, _, err := k.GetIntegerValue(o.name)
		return err == nil && uint32(n) == *v.dword
	case v.str != nil:
		s, _, err := k.GetStringValue(o.name)
		return err == nil && s == *v.str
	case v.bin != nil:
		b, _, err := k.GetBinaryValue(o.name)
		return err == nil && bytes.Equal(b, v.bin)
	}
	_, _, err = k.GetValue(o.name, nil)
	return err != nil
}

const (
	hkcu   = registry.CURRENT_USER
	hklm   = registry.LOCAL_MACHINE
	cdm    = `Software\Microsoft\Windows\CurrentVersion\ContentDeliveryManager`
	adv    = `Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`
	edge   = `SOFTWARE\Policies\Microsoft\Edge`
	mmProf = `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Multimedia\SystemProfile`
	games  = mmProf + `\Tasks\Games`
	desk   = `Control Panel\Desktop`
	mouse  = `Control Panel\Mouse`

	classicMenuKey = `Software\Classes\CLSID\{86ca1aa0-34aa-4e8b-a509-50c905bae2a2}`
	galleryKey     = `Software\Classes\CLSID\{e88865ea-0e1c-4e20-9aa6-edcd0212c87c}`
	homeKey        = `Software\Classes\CLSID\{f874310e-b6b7-47dc-bc84-b9e6b38f5903}`
)

// Источники твиков.
const (
	srcWinUtil   = "WinUtil"
	srcDebloat   = "Win11Debloat"
	srcOptimizer = "Optimizer"
	srcSophia    = "Sophia Script"
)

type settingDef struct {
	id, group        string
	ru, en, dru, den string
	cru, cen         string // предупреждение о побочном эффекте
	admin, rec       bool
	restart, src     string
	ops              []regOp
	apply, revert    func() error
	state            func() bool
	avail            func() bool // nil — доступно всегда
}

func cdmOps(names ...string) []regOp {
	var ops []regOp
	for _, n := range names {
		ops = append(ops, regOp{hkcu, cdm, n, dw(0), dw(1)})
	}
	return ops
}

func edgeOps(names ...string) []regOp {
	var ops []regOp
	for _, n := range names {
		ops = append(ops, regOp{hklm, edge, n, dw(0), none()})
	}
	return ops
}

func cat(groups ...[]regOp) []regOp {
	var out []regOp
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// Маска визуальных эффектов: «наилучшее быстродействие» и значение Windows по умолчанию.
var (
	fxPerformance = bin(0x90, 0x12, 0x03, 0x80, 0x10, 0x00, 0x00, 0x00)
	fxDefault     = bin(0x9E, 0x1E, 0x07, 0x80, 0x12, 0x00, 0x00, 0x00)
)

var settingDefs = []settingDef{
	// ===== Конфиденциальность =====
	{id: "telemetry", group: "privacy", admin: true, rec: true, src: srcWinUtil,
		ru: "Отключить телеметрию", en: "Disable telemetry",
		dru: "Минимальный уровень диагностики, запрет опросов обратной связи и отслеживания запуска программ; службы DiagTrack и dmwappushservice отключаются",
		den: "Minimum diagnostic data, no feedback prompts or app-launch tracking; DiagTrack and dmwappushservice services disabled",
		ops: []regOp{
			{hklm, `SOFTWARE\Policies\Microsoft\Windows\DataCollection`, "AllowTelemetry", dw(0), none()},
			{hklm, `SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\DataCollection`, "AllowTelemetry", dw(0), none()},
			{hklm, `SOFTWARE\Policies\Microsoft\Windows\DataCollection`, "DoNotShowFeedbackNotifications", dw(1), none()},
			{hkcu, `Software\Microsoft\Siuf\Rules`, "NumberOfSIUFInPeriod", dw(0), none()},
			{hkcu, adv, "Start_TrackProgs", dw(0), none()},
		},
		apply: func() error {
			return setServices(map[string]uint32{"DiagTrack": mgr.StartDisabled, "dmwappushservice": mgr.StartDisabled}, true)
		},
		revert: func() error {
			return setServices(map[string]uint32{"DiagTrack": mgr.StartAutomatic, "dmwappushservice": mgr.StartManual}, false)
		}},
	{id: "adid", group: "privacy", rec: true, src: srcWinUtil,
		ru: "Отключить рекламный идентификатор", en: "Disable advertising ID",
		dru: "Приложения не смогут показывать персонализированную рекламу", den: "Apps can no longer show personalized ads",
		ops: []regOp{{hkcu, `Software\Microsoft\Windows\CurrentVersion\AdvertisingInfo`, "Enabled", dw(0), dw(1)}}},
	{id: "tailored", group: "privacy", rec: true, src: srcWinUtil,
		ru: "Отключить персональные предложения", en: "Disable tailored experiences",
		dru: "Microsoft не будет подбирать советы и рекламу по диагностическим данным", den: "No tips and ads based on diagnostic data",
		ops: []regOp{{hkcu, `Software\Microsoft\Windows\CurrentVersion\Privacy`, "TailoredExperiencesWithDiagnosticDataEnabled", dw(0), dw(1)}}},
	{id: "inking", group: "privacy", rec: true, src: srcDebloat,
		ru: "Не собирать рукописный и голосовой ввод", en: "Stop collecting ink, typing and speech data",
		dru: "Отключает обучение по рукописному вводу, сбор контактов, онлайн-распознавание речи и отправку данных клавиатуры",
		den: "Turns off ink/typing personalization, contact harvesting, online speech recognition and typing insights",
		ops: []regOp{
			{hkcu, `Software\Microsoft\InputPersonalization`, "RestrictImplicitInkCollection", dw(1), dw(0)},
			{hkcu, `Software\Microsoft\InputPersonalization`, "RestrictImplicitTextCollection", dw(1), dw(0)},
			{hkcu, `Software\Microsoft\InputPersonalization\TrainedDataStore`, "HarvestContacts", dw(0), dw(1)},
			{hkcu, `Software\Microsoft\Personalization\Settings`, "AcceptedPrivacyPolicy", dw(0), dw(1)},
			{hkcu, `Software\Microsoft\Speech_OneCore\Settings\OnlineSpeechPrivacy`, "HasAccepted", dw(0), dw(1)},
			{hkcu, `Software\Microsoft\Input\TIPC`, "Enabled", dw(0), dw(1)},
		}},
	{id: "activity", group: "privacy", admin: true, rec: true, src: srcWinUtil,
		ru: "Отключить журнал действий", en: "Disable activity history",
		dru: "Windows не будет собирать историю открытых файлов и приложений", den: "Windows stops collecting history of opened files and apps",
		ops: []regOp{
			{hklm, `SOFTWARE\Policies\Microsoft\Windows\System`, "EnableActivityFeed", dw(0), none()},
			{hklm, `SOFTWARE\Policies\Microsoft\Windows\System`, "PublishUserActivities", dw(0), none()},
			{hklm, `SOFTWARE\Policies\Microsoft\Windows\System`, "UploadUserActivities", dw(0), none()},
		}},
	{id: "errorreport", group: "privacy", admin: true, rec: true, src: srcWinUtil,
		ru: "Отключить отчёты об ошибках", en: "Disable error reporting",
		dru: "Сведения о сбоях программ не отправляются в Microsoft", den: "App crash data is no longer sent to Microsoft",
		ops: []regOp{{hklm, `SOFTWARE\Policies\Microsoft\Windows\Windows Error Reporting`, "Disabled", dw(1), none()}}},
	{id: "location", group: "privacy", admin: true, src: srcWinUtil,
		ru: "Отключить определение местоположения", en: "Disable location services",
		dru: "Ни система, ни приложения не получат координаты устройства", den: "Neither Windows nor apps can get the device location",
		cru: "Перестанут работать автоматический часовой пояс, «Погода» и «Карты»", cen: "Automatic time zone, Weather and Maps will stop working",
		ops: []regOp{{hklm, `SOFTWARE\Policies\Microsoft\Windows\LocationAndSensors`, "DisableLocation", dw(1), none()}}},
	{id: "findmydevice", group: "privacy", admin: true, src: srcDebloat,
		ru: "Отключить «Поиск устройства»", en: "Disable Find My Device",
		dru: "Устройство не будет периодически сообщать своё местоположение Microsoft", den: "The device stops reporting its location to Microsoft",
		cru: "Потерянный ноутбук нельзя будет найти через аккаунт Microsoft", cen: "A lost laptop can't be located via your Microsoft account",
		ops: []regOp{{hklm, `SOFTWARE\Policies\Microsoft\FindMyDevice`, "AllowFindMyDevice", dw(0), none()}}},

	// ===== Реклама и предложения =====
	{id: "suggestions", group: "ads", rec: true, src: srcDebloat,
		ru: "Убрать рекламу, советы и автоустановку приложений", en: "Remove ads, tips and silent app installs",
		dru: "Предложения в «Пуске» и Параметрах, советы, реклама на экране блокировки, напоминания «завершите настройку», тихая установка игр вроде Candy Crush",
		den: "Start and Settings suggestions, tips, lock screen ads, “finish setting up” nags, silent installs of games like Candy Crush",
		ops: cat(cdmOps("SilentInstalledAppsEnabled", "SystemPaneSuggestionsEnabled", "SoftLandingEnabled",
			"PreInstalledAppsEnabled", "OemPreInstalledAppsEnabled", "RotatingLockScreenOverlayEnabled",
			"SubscribedContent-310093Enabled", "SubscribedContent-338387Enabled", "SubscribedContent-338388Enabled",
			"SubscribedContent-338389Enabled", "SubscribedContent-338393Enabled", "SubscribedContent-353694Enabled",
			"SubscribedContent-353696Enabled", "SubscribedContent-353698Enabled"),
			[]regOp{
				{hkcu, adv, "ShowSyncProviderNotifications", dw(0), dw(1)},
				{hkcu, adv, "Start_IrisRecommendations", dw(0), dw(1)},
				{hkcu, adv, "Start_AccountNotifications", dw(0), dw(1)},
				{hkcu, `Software\Microsoft\Windows\CurrentVersion\UserProfileEngagement`, "ScoobeSystemSettingEnabled", dw(0), dw(1)},
				{hkcu, `Software\Microsoft\Windows\CurrentVersion\SystemSettings\AccountNotifications`, "EnableAccountNotifications", dw(0), dw(1)},
				{hkcu, `Software\Microsoft\Windows\CurrentVersion\Notifications\Settings\Windows.SystemToast.Suggested`, "Enabled", dw(0), dw(1)},
			})},
	{id: "consumer", group: "ads", admin: true, rec: true, src: srcWinUtil,
		ru: "Запретить «потребительские функции»", en: "Disable consumer features",
		dru: "Windows перестаёт сама ставить рекламные приложения и игры из Store", den: "Windows stops auto-installing promoted Store apps and games",
		cru: "В редакциях Home и Pro политика действует частично", cen: "Only partially honored on Home and Pro editions",
		ops: []regOp{{hklm, `SOFTWARE\Policies\Microsoft\Windows\CloudContent`, "DisableWindowsConsumerFeatures", dw(1), none()}}},
	{id: "settingsads", group: "ads", admin: true, rec: true, src: srcDebloat,
		ru: "Убрать рекламу Microsoft 365 в Параметрах", en: "Remove Microsoft 365 ads in Settings",
		dru: "Скрывает промо-карточки подписок на главной странице «Параметров»", den: "Hides subscription promo cards on the Settings home page",
		ops: []regOp{{hklm, `SOFTWARE\Policies\Microsoft\Windows\CloudContent`, "DisableConsumerAccountStateContent", dw(1), none()}}},
	{id: "bing", group: "ads", admin: true, rec: true, src: srcDebloat,
		ru: "Убрать Bing из поиска «Пуска»", en: "Remove Bing from Start search",
		dru: "Поиск будет искать только на компьютере, без веб-результатов", den: "Search looks only on this PC, no web results",
		ops: []regOp{
			{hkcu, `Software\Microsoft\Windows\CurrentVersion\Search`, "BingSearchEnabled", dw(0), none()},
			{hkcu, `Software\Policies\Microsoft\Windows\Explorer`, "DisableSearchBoxSuggestions", dw(1), none()},
		}},
	{id: "edgeads", group: "ads", admin: true, rec: true, src: srcDebloat,
		ru: "Убрать рекламу и навязчивость в Edge", en: "Remove Edge ads and nags",
		dru: "Без ленты новостей MSN, помощника покупок, рекомендаций, запросов «сделать браузером по умолчанию» и отправки данных",
		den: "No MSN feed, shopping assistant, recommendations, default-browser nags or diagnostic data",
		ops: edgeOps("NewTabPageContentEnabled", "EdgeShoppingAssistantEnabled", "ShowRecommendationsEnabled",
			"UserFeedbackAllowed", "PersonalizationReportingEnabled", "DiagnosticData", "SpotlightExperiencesAndRecommendationsEnabled",
			"DefaultBrowserSettingsCampaignEnabled", "ShowAcrobatSubscriptionButton", "AlternateErrorPagesEnabled")},
	{id: "devicemeta", group: "ads", admin: true, rec: true, src: srcWinUtil,
		ru: "Запретить автоустановку фирменных программ устройств", en: "Block auto-install of device companion apps",
		dru: "При подключении мыши или клавиатуры Windows не будет сама скачивать Razer Synapse, Logitech и т. п.",
		den: "Plugging in a mouse or keyboard no longer auto-downloads Razer Synapse, Logitech tools, etc.",
		ops: []regOp{{hklm, `SOFTWARE\Policies\Microsoft\Windows\Device Metadata`, "PreventDeviceMetadataFromNetwork", dw(1), none()}}},

	// ===== ИИ-функции =====
	{id: "copilot", group: "ai", admin: true, rec: true, src: srcDebloat,
		ru: "Отключить Copilot", en: "Disable Copilot",
		dru: "Политика отключения Windows Copilot и его кнопки", den: "Windows Copilot turn-off policy and button",
		ops: []regOp{
			{hkcu, `Software\Policies\Microsoft\Windows\WindowsCopilot`, "TurnOffWindowsCopilot", dw(1), none()},
			{hklm, `SOFTWARE\Policies\Microsoft\Windows\WindowsCopilot`, "TurnOffWindowsCopilot", dw(1), none()},
			{hkcu, adv, "ShowCopilotButton", dw(0), none()},
		}},
	{id: "recall", group: "ai", admin: true, rec: true, src: srcDebloat,
		ru: "Отключить Recall", en: "Disable Recall",
		dru: "Запрет снимков экрана для ИИ-поиска по истории (Copilot+ ПК)", den: "Blocks AI screenshot history (Copilot+ PCs)",
		ops: []regOp{
			{hkcu, `Software\Policies\Microsoft\Windows\WindowsAI`, "DisableAIDataAnalysis", dw(1), none()},
			{hklm, `SOFTWARE\Policies\Microsoft\Windows\WindowsAI`, "DisableAIDataAnalysis", dw(1), none()},
		}},
	{id: "clicktodo", group: "ai", admin: true, rec: true, src: srcDebloat,
		ru: "Отключить Click to Do", en: "Disable Click to Do",
		dru: "ИИ-действия над содержимым экрана (Win + щелчок)", den: "AI actions on screen content (Win + click)",
		ops: []regOp{
			{hkcu, `Software\Policies\Microsoft\Windows\WindowsAI`, "DisableClickToDo", dw(1), none()},
			{hklm, `SOFTWARE\Policies\Microsoft\Windows\WindowsAI`, "DisableClickToDo", dw(1), none()},
		}},
	{id: "aiapps", group: "ai", admin: true, rec: true, src: srcDebloat,
		ru: "Отключить ИИ в Paint и Блокноте", en: "Disable AI in Paint and Notepad",
		dru: "Cocreator, генеративная заливка и стирание, удаление фона, ИИ-переписывание текста", den: "Cocreator, generative fill/erase, background removal, AI rewrite",
		ops: []regOp{
			{hklm, `SOFTWARE\Policies\WindowsNotepad`, "DisableAIFeatures", dw(1), none()},
			{hklm, `SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\Paint`, "DisableCocreator", dw(1), none()},
			{hklm, `SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\Paint`, "DisableGenerativeFill", dw(1), none()},
			{hklm, `SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\Paint`, "DisableImageCreator", dw(1), none()},
			{hklm, `SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\Paint`, "DisableGenerativeErase", dw(1), none()},
			{hklm, `SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\Paint`, "DisableRemoveBackground", dw(1), none()},
		}},
	{id: "edgeai", group: "ai", admin: true, rec: true, src: srcDebloat,
		ru: "Отключить ИИ в Edge", en: "Disable AI in Edge",
		dru: "Боковая панель Copilot, ИИ-поиск по истории, «Написать» в полях ввода, чат Bing на новой вкладке", den: "Copilot sidebar, AI history search, inline Compose, Bing chat on new tab",
		ops: edgeOps("HubsSidebarEnabled", "CopilotPageContext", "CopilotCDPPageContext", "Microsoft365CopilotChatIconEnabled",
			"EdgeHistoryAISearchEnabled", "ComposeInlineEnabled", "NewTabPageBingChatEnabled")},
	{id: "aiservice", group: "ai", admin: true, src: srcDebloat, restart: "reboot",
		ru: "Не запускать службу ИИ автоматически", en: "Don't auto-start the AI service",
		dru: "Служба WSAIFabricSvc запускается только по требованию — меньше фоновой нагрузки", den: "WSAIFabricSvc starts on demand only — less background load",
		apply:  func() error { return setServiceStart("WSAIFabricSvc", mgr.StartManual) },
		revert: func() error { return setServiceStart("WSAIFabricSvc", mgr.StartAutomatic) },
		state:  func() bool { return serviceStart("WSAIFabricSvc") == mgr.StartManual },
		avail:  func() bool { return serviceStart("WSAIFabricSvc") != 0xFFFF }},
	{id: "cortana", group: "ai", admin: true, rec: true, src: srcWinUtil,
		ru: "Отключить Кортану", en: "Disable Cortana",
		dru: "Запрет голосового помощника политикой", den: "Blocks the voice assistant by policy",
		ops: []regOp{{hklm, `SOFTWARE\Policies\Microsoft\Windows\Windows Search`, "AllowCortana", dw(0), none()}}},

	// ===== Панель задач и «Пуск» =====
	{id: "widgets", group: "taskbar", admin: true, rec: true, src: srcWinUtil,
		ru: "Отключить виджеты", en: "Disable widgets",
		dru: "Убирает панель новостей и виджетов с панели задач и экрана блокировки", den: "Removes the news and widgets board from the taskbar and lock screen",
		ops: []regOp{{hklm, `SOFTWARE\Policies\Microsoft\Dsh`, "AllowNewsAndInterests", dw(0), none()}}},
	{id: "endtask", group: "taskbar", rec: true, src: srcWinUtil,
		ru: "«Завершить задачу» в меню панели задач", en: "“End task” in taskbar menu",
		dru: "Зависшую программу можно закрыть правым щелчком по её значку (Windows 11)", den: "Close a frozen app by right-clicking its taskbar icon (Windows 11)",
		ops: []regOp{{hkcu, adv + `\TaskbarDeveloperSettings`, "TaskbarEndTask", dw(1), dw(0)}}},
	{id: "lastactive", group: "taskbar", src: srcDebloat,
		ru: "Щелчок по значку — последнее окно", en: "Taskbar click opens last active window",
		dru: "Если у программы несколько окон, щелчок сразу открывает последнее активное", den: "With several windows, a click opens the last active one",
		ops: []regOp{{hkcu, adv, "LastActiveClick", dw(1), none()}}},
	{id: "taskbarleft", group: "taskbar", src: srcDebloat, restart: "explorer",
		ru: "Значки панели задач слева", en: "Left-align taskbar icons",
		dru: "Кнопка «Пуск» и значки — в левом углу, как в Windows 10", den: "Start button and icons on the left, like Windows 10",
		ops: []regOp{{hkcu, adv, "TaskbarAl", dw(0), dw(1)}}},
	{id: "taskview", group: "taskbar", src: srcDebloat,
		ru: "Скрыть кнопку «Представление задач»", en: "Hide the Task View button",
		dru: "Виртуальные рабочие столы по-прежнему доступны по Win+Tab", den: "Virtual desktops are still available via Win+Tab",
		ops: []regOp{{hkcu, adv, "ShowTaskViewButton", dw(0), dw(1)}}},
	{id: "searchicon", group: "taskbar", src: srcDebloat,
		ru: "Поиск на панели задач — только значок", en: "Taskbar search as icon only",
		dru: "Освобождает место на панели задач вместо широкого поля поиска", den: "Saves taskbar space instead of a wide search box",
		ops: []regOp{{hkcu, `Software\Microsoft\Windows\CurrentVersion\Search`, "SearchboxTaskbarMode", dw(1), dw(2)}}},
	{id: "seconds", group: "taskbar", src: srcSophia,
		ru: "Показывать секунды в часах", en: "Show seconds in the clock",
		dru: "Часы в трее показывают секунды", den: "The tray clock shows seconds",
		ops: []regOp{{hkcu, adv, "ShowSecondsInSystemClock", dw(1), dw(0)}}},
	{id: "startrec", group: "taskbar", admin: true, src: srcDebloat,
		ru: "Скрыть раздел «Рекомендуем» в «Пуске»", en: "Hide “Recommended” in Start",
		dru: "Убирает список недавних файлов и рекламных рекомендаций из меню «Пуск»", den: "Removes recent files and promoted items from Start",
		cru: "В некоторых редакциях Windows 11 политика не действует", cen: "The policy has no effect on some Windows 11 editions",
		ops: []regOp{
			{hkcu, `Software\Policies\Microsoft\Windows\Explorer`, "HideRecommendedSection", dw(1), none()},
			{hklm, `SOFTWARE\Policies\Microsoft\Windows\Explorer`, "HideRecommendedSection", dw(1), none()},
			{hkcu, adv, "Start_TrackDocs", dw(0), none()},
		}},
	{id: "phonestart", group: "taskbar", src: srcDebloat,
		ru: "Убрать «Связь с телефоном» из «Пуска»", en: "Remove Phone Link from Start",
		dru: "Скрывает панель смартфона рядом с меню «Пуск»", den: "Hides the phone panel next to the Start menu",
		ops: []regOp{{hkcu, `Software\Microsoft\Windows\CurrentVersion\Start\Companions\Microsoft.YourPhone_8wekyb3d8bbwe`, "IsEnabled", dw(0), none()}}},

	// ===== Проводник =====
	{id: "fileext", group: "explorer", rec: true, src: srcWinUtil, restart: "explorer",
		ru: "Показывать расширения файлов", en: "Show file extensions",
		dru: "Защищает от файлов-обманок вида «фото.jpg.exe»", den: "Protects against fake files like photo.jpg.exe",
		ops: []regOp{{hkcu, adv, "HideFileExt", dw(0), dw(1)}}},
	{id: "hidden", group: "explorer", src: srcWinUtil, restart: "explorer",
		ru: "Показывать скрытые файлы", en: "Show hidden files",
		dru: "Видны папки вроде AppData — удобно для ручной очистки", den: "Folders like AppData become visible — handy for manual cleanup",
		ops: []regOp{{hkcu, adv, "Hidden", dw(1), dw(2)}}},
	{id: "classicmenu", group: "explorer", src: srcDebloat, restart: "explorer",
		ru: "Классическое контекстное меню", en: "Classic context menu",
		dru: "Полное меню правой кнопки без «Показать дополнительные параметры» (Windows 11)", den: "Full right-click menu without “Show more options” (Windows 11)",
		apply: func() error {
			k, _, err := registry.CreateKey(hkcu, classicMenuKey+`\InprocServer32`, registry.SET_VALUE)
			if err != nil {
				return err
			}
			defer k.Close()
			return k.SetStringValue("", "")
		},
		revert: func() error {
			_ = registry.DeleteKey(hkcu, classicMenuKey+`\InprocServer32`)
			if err := registry.DeleteKey(hkcu, classicMenuKey); err != nil && !errors.Is(err, registry.ErrNotExist) {
				return err
			}
			return nil
		},
		state: func() bool {
			k, err := registry.OpenKey(hkcu, classicMenuKey+`\InprocServer32`, registry.QUERY_VALUE)
			if err != nil {
				return false
			}
			k.Close()
			return true
		}},
	{id: "thispc", group: "explorer", src: srcWinUtil,
		ru: "Открывать Проводник на «Этот компьютер»", en: "Open File Explorer to This PC",
		dru: "Сразу видны диски вместо «Главной» с недавними файлами", den: "Shows drives instead of Home with recent files",
		ops: []regOp{{hkcu, adv, "LaunchTo", dw(1), none()}}},
	{id: "nogallery", group: "explorer", src: srcDebloat, restart: "explorer",
		ru: "Скрыть «Галерею» в Проводнике", en: "Hide Gallery in File Explorer",
		dru: "Убирает пункт «Галерея» из панели навигации (Windows 11)", den: "Removes Gallery from the navigation pane (Windows 11)",
		ops: []regOp{{hkcu, galleryKey, "System.IsPinnedToNameSpaceTree", dw(0), none()}}},
	{id: "nohome", group: "explorer", src: srcWinUtil, restart: "explorer",
		ru: "Скрыть «Главную» в Проводнике", en: "Hide Home in File Explorer",
		dru: "Убирает «Главную» с недавними файлами из панели навигации", den: "Removes Home with recent files from the navigation pane",
		ops: []regOp{{hkcu, homeKey, "System.IsPinnedToNameSpaceTree", dw(0), none()}}},
	{id: "norecent", group: "explorer", src: srcSophia,
		ru: "Не показывать недавние файлы и папки", en: "Don't show recent files and folders",
		dru: "Быстрый доступ не запоминает, что вы открывали", den: "Quick access stops remembering what you opened",
		ops: []regOp{
			{hkcu, `Software\Microsoft\Windows\CurrentVersion\Explorer`, "ShowRecent", dw(0), none()},
			{hkcu, `Software\Microsoft\Windows\CurrentVersion\Explorer`, "ShowFrequent", dw(0), none()},
		}},

	// ===== Производительность =====
	{id: "visualfx", group: "perf", src: srcWinUtil, restart: "logoff",
		ru: "Визуальные эффекты: наилучшее быстродействие", en: "Visual effects: best performance",
		dru: "Отключает анимации окон и панели задач, тени, Aero Peek и прозрачное выделение — заметно на слабых ПК",
		den: "Turns off window/taskbar animations, shadows, Aero Peek and translucent selection — noticeable on slow PCs",
		cru: "Интерфейс станет проще на вид", cen: "The UI will look plainer",
		ops: []regOp{
			{hkcu, `Software\Microsoft\Windows\CurrentVersion\Explorer\VisualEffects`, "VisualFXSetting", dw(3), none()},
			{hkcu, desk, "UserPreferencesMask", fxPerformance, fxDefault},
			{hkcu, desk + `\WindowMetrics`, "MinAnimate", sz("0"), sz("1")},
			{hkcu, desk, "DragFullWindows", sz("0"), sz("1")},
			{hkcu, adv, "TaskbarAnimations", dw(0), dw(1)},
			{hkcu, adv, "ListviewAlphaSelect", dw(0), dw(1)},
			{hkcu, adv, "ListviewShadow", dw(0), dw(1)},
			{hkcu, `Software\Microsoft\Windows\DWM`, "EnableAeroPeek", dw(0), dw(1)},
		}},
	{id: "transparency", group: "perf", src: srcDebloat,
		ru: "Отключить прозрачность", en: "Disable transparency",
		dru: "Меньше нагрузки на видеокарту от эффектов размытия", den: "Less GPU load from blur effects",
		ops: []regOp{{hkcu, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, "EnableTransparency", dw(0), dw(1)}}},
	{id: "menudelay", group: "perf", rec: true, src: srcOptimizer,
		ru: "Ускорить отклик меню", en: "Faster menu response",
		dru: "Подменю открываются сразу вместо задержки 400 мс", den: "Submenus open instantly instead of after 400 ms",
		ops: []regOp{{hkcu, desk, "MenuShowDelay", sz("0"), sz("400")}}},
	{id: "bgapps", group: "perf", src: srcWinUtil,
		ru: "Запретить фоновую работу приложений", en: "Block background apps",
		dru: "Приложения из Microsoft Store не работают в фоне", den: "Microsoft Store apps don't run in the background",
		cru: "Могут перестать приходить уведомления из таких приложений", cen: "Notifications from such apps may stop",
		ops: []regOp{{hkcu, `Software\Microsoft\Windows\CurrentVersion\BackgroundAccessApplications`, "GlobalUserDisabled", dw(1), dw(0)}}},
	{id: "hibernate", group: "perf", admin: true, src: srcWinUtil,
		ru: "Отключить гибернацию", en: "Disable hibernation",
		dru: "Удаляет hiberfil.sys и освобождает на диске объём около 40% ОЗУ", den: "Deletes hiberfil.sys, freeing about 40% of RAM size on disk",
		cru: "Исчезнет режим гибернации и быстрый запуск", cen: "Hibernate and Fast Startup will be unavailable",
		apply:  func() error { return powercfgHibernate(false) },
		revert: func() error { return powercfgHibernate(true) },
		state: func() bool {
			return regOp{hklm, `SYSTEM\CurrentControlSet\Control\Power`, "HibernateEnabled", dw(0), none()}.is(dw(0))
		}},
	{id: "faststartup", group: "perf", admin: true, src: srcDebloat,
		ru: "Отключить быстрый запуск", en: "Disable Fast Startup",
		dru: "«Завершение работы» становится полным: меньше странных сбоев драйверов и проблем с обновлениями", den: "Shutdown becomes a full shutdown: fewer driver glitches and update issues",
		cru: "Включение компьютера может стать на несколько секунд дольше", cen: "Boot may take a few seconds longer",
		ops: []regOp{{hklm, `SYSTEM\CurrentControlSet\Control\Session Manager\Power`, "HiberbootEnabled", dw(0), dw(1)}}},

	// ===== Игры =====
	{id: "gamedvr", group: "gaming", rec: true, src: srcWinUtil,
		ru: "Отключить фоновую запись игр", en: "Disable background game recording",
		dru: "Game DVR постоянно пишет видео в фоне и отнимает FPS", den: "Game DVR constantly records video in the background, costing FPS",
		ops: []regOp{
			{hkcu, `System\GameConfigStore`, "GameDVR_Enabled", dw(0), dw(1)},
			{hkcu, `Software\Microsoft\Windows\CurrentVersion\GameDVR`, "AppCaptureEnabled", dw(0), dw(1)},
		}},
	{id: "gamemode", group: "gaming", rec: true, src: srcWinUtil,
		ru: "Включить игровой режим", en: "Enable Game Mode",
		dru: "Windows отдаёт приоритет игре и откладывает фоновые задачи", den: "Windows prioritizes the game and defers background work",
		ops: []regOp{
			{hkcu, `Software\Microsoft\GameBar`, "AutoGameModeEnabled", dw(1), dw(0)},
			{hkcu, `Software\Microsoft\GameBar`, "AllowAutoGameMode", dw(1), dw(0)},
		}},
	{id: "gamesched", group: "gaming", admin: true, src: srcOptimizer,
		ru: "Приоритет игр в планировщике мультимедиа", en: "Prioritize games in the multimedia scheduler",
		dru: "Играм — высокий приоритет ЦП и диска, фоновым задачам резервируется 10% вместо 20%", den: "Games get high CPU and I/O priority; background reserve drops from 20% to 10%",
		ops: []regOp{
			{hklm, mmProf, "SystemResponsiveness", dw(10), dw(20)},
			{hklm, games, "GPU Priority", dw(8), dw(8)},
			{hklm, games, "Priority", dw(6), dw(2)},
			{hklm, games, "Scheduling Category", sz("High"), sz("Medium")},
			{hklm, games, "SFIO Priority", sz("High"), sz("Normal")},
		}},
	{id: "netthrottle", group: "gaming", admin: true, src: srcOptimizer, restart: "reboot",
		ru: "Отключить сетевое ограничение мультимедиа", en: "Disable multimedia network throttling",
		dru: "Windows перестаёт искусственно ограничивать сеть во время воспроизведения звука и видео", den: "Windows stops throttling the network while audio/video is playing",
		cru: "Эффект заметен не везде — проверьте пинг до и после", cen: "Not noticeable everywhere — compare ping before and after",
		ops: []regOp{{hklm, mmProf, "NetworkThrottlingIndex", dw(0xFFFFFFFF), dw(10)}}},
	{id: "hags", group: "gaming", admin: true, src: srcOptimizer, restart: "reboot",
		ru: "Аппаратное планирование GPU", en: "Hardware-accelerated GPU scheduling",
		dru: "Видеокарта сама управляет своей памятью — меньше задержка ввода", den: "The GPU manages its own memory — lower input latency",
		cru: "Нужна видеокарта NVIDIA GTX 1000+ / AMD RX 5000+ с актуальным драйвером", cen: "Requires NVIDIA GTX 1000+ / AMD RX 5000+ with a current driver",
		ops: []regOp{{hklm, `SYSTEM\CurrentControlSet\Control\GraphicsDrivers`, "HwSchMode", dw(2), dw(1)}}},
	{id: "mouseaccel", group: "gaming", rec: true, src: srcDebloat, restart: "logoff",
		ru: "Отключить ускорение мыши", en: "Disable mouse acceleration",
		dru: "Курсор двигается строго пропорционально движению мыши — точнее прицеливание", den: "Pointer moves 1:1 with the mouse — more precise aiming",
		ops: []regOp{
			{hkcu, mouse, "MouseSpeed", sz("0"), sz("1")},
			{hkcu, mouse, "MouseThreshold1", sz("0"), sz("6")},
			{hkcu, mouse, "MouseThreshold2", sz("0"), sz("10")},
		}},
	{id: "stickykeys", group: "gaming", rec: true, src: srcDebloat,
		ru: "Отключить залипание клавиш по Shift×5", en: "Disable Sticky Keys shortcut",
		dru: "Пять нажатий Shift в игре больше не вызывают окно залипания", den: "Pressing Shift five times no longer pops up Sticky Keys",
		ops: []regOp{{hkcu, `Control Panel\Accessibility\StickyKeys`, "Flags", sz("506"), sz("510")}}},

	// ===== Обновления =====
	{id: "noautoreboot", group: "update", admin: true, rec: true, src: srcDebloat,
		ru: "Не перезагружать после обновлений, пока вы в системе", en: "No auto-restart after updates while signed in",
		dru: "Windows не перезапустит компьютер сама, пока вы работаете", den: "Windows won't restart on its own while you're signed in",
		ops: []regOp{{hklm, `SOFTWARE\Policies\Microsoft\Windows\WindowsUpdate\AU`, "NoAutoRebootWithLoggedOnUsers", dw(1), none()}}},
	{id: "deliveryopt", group: "update", admin: true, rec: true, src: srcWinUtil,
		ru: "Отключить раздачу обновлений другим ПК", en: "Disable update sharing (Delivery Optimization)",
		dru: "Компьютер не раздаёт скачанные обновления через интернет — экономит трафик", den: "The PC stops uploading updates to others over the internet — saves bandwidth",
		ops: []regOp{{hklm, `SOFTWARE\Policies\Microsoft\Windows\DeliveryOptimization`, "DODownloadMode", dw(0), none()}}},

	// ===== Система =====
	{id: "darkmode", group: "system", src: srcWinUtil,
		ru: "Тёмная тема Windows", en: "Windows dark mode",
		dru: "Тёмное оформление системы и приложений", den: "Dark theme for the system and apps",
		ops: []regOp{
			{hkcu, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, "AppsUseLightTheme", dw(0), dw(1)},
			{hkcu, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, "SystemUsesLightTheme", dw(0), dw(1)},
		}},
	{id: "longpaths", group: "system", admin: true, rec: true, src: srcWinUtil,
		ru: "Разрешить длинные пути (> 260 символов)", en: "Enable long paths (> 260 chars)",
		dru: "Убирает ошибки «Слишком длинное имя файла» при копировании глубоких папок", den: "Fixes “file name too long” errors with deeply nested folders",
		ops: []regOp{{hklm, `SYSTEM\CurrentControlSet\Control\FileSystem`, "LongPathsEnabled", dw(1), dw(0)}}},
	{id: "numlock", group: "system", src: srcWinUtil,
		ru: "Num Lock включён при входе", en: "Num Lock on at sign-in",
		dru: "Цифровая клавиатура сразу работает", den: "The numeric keypad works right away",
		ops: []regOp{{hkcu, `Control Panel\Keyboard`, "InitialKeyboardIndicators", sz("2"), sz("0")}}},
	{id: "wpbt", group: "system", admin: true, src: srcWinUtil, restart: "reboot",
		ru: "Запретить программы из прошивки (WPBT)", en: "Block firmware-injected software (WPBT)",
		dru: "Прошивка материнской платы не сможет сама устанавливать фирменный софт производителя в Windows", den: "Motherboard firmware can no longer silently install vendor software into Windows",
		ops: []regOp{{hklm, `SYSTEM\CurrentControlSet\Control\Session Manager`, "DisableWpbtExecution", dw(1), none()}}},
	{id: "verbosebsod", group: "system", admin: true, src: srcWinUtil,
		ru: "Подробный «синий экран»", en: "Detailed blue screen",
		dru: "При сбое показываются технические параметры — проще найти виновный драйвер", den: "Crash screens show technical details — easier to find the faulty driver",
		ops: []regOp{
			{hklm, `SYSTEM\CurrentControlSet\Control\CrashControl`, "DisplayParameters", dw(1), none()},
			{hklm, `SYSTEM\CurrentControlSet\Control\CrashControl`, "DisableEmoticon", dw(1), none()},
		}},

	// ===== Защита (с осторожностью) =====
	{id: "defender", group: "security", admin: true, restart: "reboot",
		ru: "Отключить Microsoft Defender", en: "Disable Microsoft Defender",
		dru: "Полностью выключает встроенный антивирус через групповую политику: защиту в реальном времени, мониторинг поведения и облачную защиту",
		den: "Fully turns off the built-in antivirus via Group Policy: real-time protection, behaviour monitoring and cloud protection",
		cru: "ОПАСНО: компьютер останется без антивируса. Сначала выключите «Защиту от подмены» в «Безопасность Windows → Защита от вирусов и угроз → Управление настройками», иначе Windows не даст применить твик. Нужна перезагрузка",
		cen: "DANGER: the PC will have no antivirus. First turn off Tamper Protection in Windows Security → Virus & threat protection → Manage settings, or Windows will block this. A reboot is required",
		ops: []regOp{
			{hklm, `SOFTWARE\Policies\Microsoft\Windows Defender`, "DisableAntiSpyware", dw(1), none()},
			{hklm, `SOFTWARE\Policies\Microsoft\Windows Defender\Real-Time Protection`, "DisableRealtimeMonitoring", dw(1), none()},
			{hklm, `SOFTWARE\Policies\Microsoft\Windows Defender\Real-Time Protection`, "DisableBehaviorMonitoring", dw(1), none()},
			{hklm, `SOFTWARE\Policies\Microsoft\Windows Defender\Spynet`, "SpyNetReporting", dw(0), none()},
			{hklm, `SOFTWARE\Policies\Microsoft\Windows Defender\Spynet`, "SubmitSamplesConsent", dw(2), none()},
		}},
}

// ---------- Вспомогательные функции ----------

func powercfgHibernate(on bool) error {
	arg := "off"
	if on {
		arg = "on"
	}
	cmd := exec.Command("powercfg", "/hibernate", arg)
	hidden(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("powercfg: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func serviceStart(name string) uint32 {
	m, err := mgr.Connect()
	if err != nil {
		return 0xFFFF
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return 0xFFFF
	}
	defer s.Close()
	cfg, err := s.Config()
	if err != nil {
		return 0xFFFF
	}
	return cfg.StartType
}

// setServiceStart меняет тип запуска службы; отсутствующие службы пропускаются.
func setServiceStart(name string, start uint32) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return nil
	}
	defer s.Close()
	cfg, err := s.Config()
	if err != nil {
		return err
	}
	cfg.StartType = start
	return s.UpdateConfig(cfg)
}

// setServices задаёт тип запуска нескольким службам; stop — сразу остановить их.
func setServices(svcs map[string]uint32, stop bool) error {
	for name, start := range svcs {
		if err := setServiceStart(name, start); err != nil {
			return err
		}
		if m, err := mgr.Connect(); err == nil {
			if s, err := m.OpenService(name); err == nil {
				if stop {
					_, _ = s.Control(svc.Stop)
				} else if start == mgr.StartAutomatic {
					_ = s.Start()
				}
				s.Close()
			}
			m.Disconnect()
		}
	}
	return nil
}

func (d settingDef) enabled() bool {
	if d.state != nil {
		return d.state()
	}
	for _, o := range d.ops {
		if !o.is(o.on) {
			return false
		}
	}
	return len(d.ops) > 0
}

// ListSettings возвращает системные твики и их текущее состояние.
func ListSettings() []Setting {
	out := make([]Setting, 0, len(settingDefs))
	for _, d := range settingDefs {
		if d.avail != nil && !d.avail() {
			continue
		}
		out = append(out, Setting{
			ID: d.id, Group: d.group, Title: i18n.Pick(d.ru, d.en), Desc: i18n.Pick(d.dru, d.den),
			Caution: i18n.Pick(d.cru, d.cen), Admin: d.admin, Recommended: d.rec,
			Restart: d.restart, Source: d.src, Enabled: d.enabled(),
		})
	}
	return out
}

// SetSetting применяет (on=true) или откатывает твик.
func SetSetting(id string, on bool) error {
	for _, d := range settingDefs {
		if d.id != id {
			continue
		}
		if d.admin && !ctl.IsElevated() {
			return errors.New(i18n.T("нужны права администратора — нажмите «Перезапустить от имени администратора»"))
		}
		for _, o := range d.ops {
			v := o.off
			if on {
				v = o.on
			}
			if err := o.set(v); err != nil {
				if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
					return errors.New(i18n.T("доступ к реестру запрещён — нужны права администратора"))
				}
				return err
			}
		}
		if on && d.apply != nil {
			return d.apply()
		}
		if !on && d.revert != nil {
			return d.revert()
		}
		return nil
	}
	return errors.New(i18n.T("неизвестный твик"))
}
