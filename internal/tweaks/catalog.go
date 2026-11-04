// Package tweaks — удаление встроенных приложений Windows и системные твики.
package tweaks

import "github.com/healcore/healcoreops/internal/i18n"

// App — встроенное (предустановленное) приложение Windows.
type App struct {
	ID          string   `json:"id"`
	Group       string   `json:"group"`
	Name        string   `json:"name"`
	Desc        string   `json:"desc"`
	Caution     string   `json:"caution,omitempty"`
	Recommended bool     `json:"recommended"`
	Installed   bool     `json:"installed"`
	Packages    []string `json:"packages"`
}

// Setting — системный твик (вкл. = твик применён).
type Setting struct {
	ID          string `json:"id"`
	Group       string `json:"group"`
	Title       string `json:"title"`
	Desc        string `json:"desc"`
	Caution     string `json:"caution,omitempty"` // побочный эффект
	Admin       bool   `json:"admin"`
	Recommended bool   `json:"recommended"`       // входит в безопасный пресет
	Restart     string `json:"restart,omitempty"` // explorer | logoff | reboot
	Source      string `json:"source,omitempty"`  // откуда взят твик
	Enabled     bool   `json:"enabled"`
}

// Result — итог операции над элементом.
type Result struct {
	ID  string `json:"id"`
	OK  bool   `json:"ok"`
	Msg string `json:"msg"`
}

type appDef struct {
	id, group        string
	ru, en, dru, den string
	cru, cen         string
	rec              bool
	pkgs             []string
}

const oneDriveID = "onedrive"

var appDefs = []appDef{
	// Реклама и сторонние предустановки
	{id: "candy", group: "ads", ru: "Candy Crush и другие игры King", en: "Candy Crush & other King games",
		dru: "Рекламные игры, которые Windows ставит сама", den: "Promotional games installed by Windows",
		rec: true, pkgs: []string{"king.com.CandyCrushSaga", "king.com.CandyCrushSodaSaga", "king.com.BubbleWitch3Saga", "king.com.CandyCrushFriends"}},
	{id: "solitaire", group: "ads", ru: "Microsoft Solitaire Collection", en: "Microsoft Solitaire Collection",
		dru: "Пасьянсы с рекламой и подпиской", den: "Card games with ads and subscription",
		rec: true, pkgs: []string{"Microsoft.MicrosoftSolitaireCollection"}},
	{id: "spotify", group: "ads", ru: "Spotify (предустановка)", en: "Spotify (preinstalled)",
		dru: "Ярлык-заглушка или предустановленный клиент", den: "Preinstalled stub or client",
		pkgs: []string{"SpotifyAB.SpotifyMusic"}},
	{id: "disney", group: "ads", ru: "Disney+", en: "Disney+", dru: "Предустановленное приложение стриминга", den: "Preinstalled streaming app",
		rec: true, pkgs: []string{"Disney.37853FC22B2CE"}},
	{id: "netflix", group: "ads", ru: "Netflix", en: "Netflix", dru: "Предустановленное приложение стриминга", den: "Preinstalled streaming app",
		pkgs: []string{"4DF9E0F8.Netflix"}},
	{id: "primevideo", group: "ads", ru: "Prime Video", en: "Prime Video", dru: "Предустановленное приложение стриминга", den: "Preinstalled streaming app",
		rec: true, pkgs: []string{"AmazonVideo.PrimeVideo"}},
	{id: "tiktok", group: "ads", ru: "TikTok", en: "TikTok", dru: "Предустановленное приложение", den: "Preinstalled app",
		rec: true, pkgs: []string{"BytedancePte.Ltd.TikTok"}},
	{id: "facebook", group: "ads", ru: "Facebook и Instagram", en: "Facebook & Instagram", dru: "Предустановленные клиенты соцсетей", den: "Preinstalled social media clients",
		rec: true, pkgs: []string{"Facebook.Facebook", "FACEBOOK.317180B0BB486", "Facebook.InstagramBeta", "Facebook.317180B0BB486"}},
	{id: "mcafee", group: "ads", ru: "McAfee (пробная версия)", en: "McAfee (trial)", dru: "Пробный антивирус от производителя ПК", den: "OEM trial antivirus",
		rec: true, pkgs: []string{"5A894077.McAfeeSecurity"}},

	// Xbox
	{id: "xboxapp", group: "xbox", ru: "Приложение Xbox", en: "Xbox app", dru: "Магазин и библиотека Game Pass", den: "Game Pass store and library",
		cru: "Нужно для запуска игр Game Pass", cen: "Required for Game Pass games",
		pkgs: []string{"Microsoft.XboxApp", "Microsoft.GamingApp"}},
	{id: "gamebar", group: "xbox", ru: "Xbox Game Bar", en: "Xbox Game Bar", dru: "Оверлей Win+G и фоновая запись игр", den: "Win+G overlay and background game recording",
		cru: "Пропадёт запись экрана через Win+G", cen: "Win+G screen recording will stop working",
		pkgs: []string{"Microsoft.XboxGamingOverlay", "Microsoft.XboxGameOverlay", "Microsoft.XboxSpeechToTextOverlay"}},
	{id: "xboxid", group: "xbox", ru: "Службы входа Xbox", en: "Xbox sign-in services", dru: "Вход в аккаунт Xbox в играх", den: "Xbox account sign-in inside games",
		cru: "Некоторые игры перестанут входить в аккаунт", cen: "Some games will fail to sign in",
		pkgs: []string{"Microsoft.XboxIdentityProvider", "Microsoft.Xbox.TCUI"}},

	// Мультимедиа
	{id: "movies", group: "media", ru: "Кино и ТВ", en: "Movies & TV", dru: "Магазин видео Microsoft", den: "Microsoft video store",
		rec: true, pkgs: []string{"Microsoft.ZuneVideo"}},
	{id: "groove", group: "media", ru: "Groove / Медиаплеер", en: "Groove / Media Player", dru: "Музыкальный плеер", den: "Music player",
		cru: "В Windows 11 это стандартный медиаплеер", cen: "On Windows 11 this is the default media player",
		pkgs: []string{"Microsoft.ZuneMusic"}},
	{id: "viewer3d", group: "media", ru: "Средство 3D-просмотра", en: "3D Viewer", dru: "Просмотр 3D-моделей", den: "3D model viewer",
		rec: true, pkgs: []string{"Microsoft.Microsoft3DViewer"}},
	{id: "paint3d", group: "media", ru: "Paint 3D", en: "Paint 3D", dru: "Устаревший редактор (обычный Paint не затрагивается)", den: "Legacy editor (classic Paint is not affected)",
		rec: true, pkgs: []string{"Microsoft.MSPaint"}},
	{id: "mixedreality", group: "media", ru: "Портал смешанной реальности", en: "Mixed Reality Portal", dru: "Для VR-шлемов Windows MR", den: "For Windows MR headsets",
		rec: true, pkgs: []string{"Microsoft.MixedReality.Portal"}},
	{id: "print3d", group: "media", ru: "3D Builder / Print 3D", en: "3D Builder / Print 3D", dru: "Подготовка к 3D-печати", den: "3D printing preparation",
		rec: true, pkgs: []string{"Microsoft.Print3D", "Microsoft.3DBuilder"}},
	{id: "clipchamp", group: "media", ru: "Clipchamp", en: "Clipchamp", dru: "Видеоредактор с подпиской", den: "Subscription video editor",
		rec: true, pkgs: []string{"Clipchamp.Clipchamp"}},
	{id: "soundrec", group: "media", ru: "Звукозапись", en: "Sound Recorder", dru: "Запись голоса", den: "Voice recorder",
		pkgs: []string{"Microsoft.WindowsSoundRecorder"}},

	// Новости и Bing
	{id: "news", group: "bing", ru: "Новости (Microsoft News)", en: "Microsoft News", dru: "Лента новостей MSN", den: "MSN news feed",
		rec: true, pkgs: []string{"Microsoft.BingNews"}},
	{id: "weather", group: "bing", ru: "Погода (MSN)", en: "MSN Weather", dru: "Прогноз погоды MSN", den: "MSN weather forecast",
		rec: true, pkgs: []string{"Microsoft.BingWeather"}},
	{id: "bingmisc", group: "bing", ru: "Финансы и Спорт (MSN)", en: "MSN Money & Sports", dru: "Устаревшие приложения MSN", den: "Legacy MSN apps",
		rec: true, pkgs: []string{"Microsoft.BingFinance", "Microsoft.BingSports", "Microsoft.BingTravel", "Microsoft.BingFoodAndDrink", "Microsoft.BingHealthAndFitness"}},
	{id: "bingsearch", group: "bing", ru: "Поиск Bing", en: "Bing Search", dru: "Отдельное приложение поиска Bing", den: "Standalone Bing search app",
		rec: true, pkgs: []string{"Microsoft.BingSearch"}},

	// Офис и связь
	{id: "officehub", group: "office", ru: "Microsoft 365 (Office Hub)", en: "Microsoft 365 (Office Hub)", dru: "Реклама подписки Office", den: "Office subscription promo",
		rec: true, pkgs: []string{"Microsoft.MicrosoftOfficeHub"}},
	{id: "onenote", group: "office", ru: "OneNote для Windows 10", en: "OneNote for Windows 10", dru: "Устаревшая версия OneNote", den: "Legacy OneNote version",
		pkgs: []string{"Microsoft.Office.OneNote"}},
	{id: "mail", group: "office", ru: "Почта и Календарь", en: "Mail and Calendar", dru: "Старое почтовое приложение", den: "Legacy mail app",
		pkgs: []string{"microsoft.windowscommunicationsapps"}},
	{id: "outlooknew", group: "office", ru: "Outlook (новый)", en: "Outlook (new)", dru: "Веб-клиент Outlook", den: "Web-based Outlook client",
		pkgs: []string{"Microsoft.OutlookForWindows"}},
	{id: "teams", group: "office", ru: "Teams (личный)", en: "Teams (personal)", dru: "Чат Teams в панели задач", den: "Teams chat on the taskbar",
		rec: true, pkgs: []string{"MicrosoftTeams", "MSTeams"}},
	{id: "skype", group: "office", ru: "Skype", en: "Skype", dru: "Сервис закрыт Microsoft", den: "Service discontinued by Microsoft",
		rec: true, pkgs: []string{"Microsoft.SkypeApp"}},
	{id: "people", group: "office", ru: "Люди и Сообщения", en: "People & Messaging", dru: "Устаревшие приложения контактов", den: "Legacy contacts apps",
		rec: true, pkgs: []string{"Microsoft.People", "Microsoft.Messaging", "Microsoft.OneConnect"}},
	{id: "todo", group: "office", ru: "Microsoft To Do", en: "Microsoft To Do", dru: "Списки задач", den: "Task lists",
		pkgs: []string{"Microsoft.Todos"}},
	{id: "phonelink", group: "office", ru: "Связь с телефоном", en: "Phone Link", dru: "Синхронизация со смартфоном", den: "Smartphone sync",
		cru: "Пропадёт связка с Android/iPhone", cen: "Android/iPhone linking will stop working",
		pkgs: []string{"Microsoft.YourPhone", "MicrosoftWindows.CrossDevice"}},

	// ИИ
	{id: "copilot", group: "ai", ru: "Copilot", en: "Copilot", dru: "ИИ-ассистент Microsoft", den: "Microsoft AI assistant",
		rec: true, pkgs: []string{"Microsoft.Copilot", "Microsoft.Windows.Ai.Copilot.Provider"}},
	{id: "cortana", group: "ai", ru: "Кортана", en: "Cortana", dru: "Голосовой помощник (отключён Microsoft)", den: "Voice assistant (retired by Microsoft)",
		rec: true, pkgs: []string{"Microsoft.549981C3F5F10"}},

	// Утилиты
	{id: "gethelp", group: "tools", ru: "Техническая поддержка", en: "Get Help", dru: "Справочный чат Microsoft", den: "Microsoft help chat",
		rec: true, pkgs: []string{"Microsoft.GetHelp"}},
	{id: "tips", group: "tools", ru: "Советы", en: "Tips", dru: "Подсказки по Windows", den: "Windows tips",
		rec: true, pkgs: []string{"Microsoft.Getstarted"}},
	{id: "feedback", group: "tools", ru: "Центр отзывов", en: "Feedback Hub", dru: "Отправка отзывов Microsoft", den: "Send feedback to Microsoft",
		rec: true, pkgs: []string{"Microsoft.WindowsFeedbackHub"}},
	{id: "maps", group: "tools", ru: "Карты", en: "Maps", dru: "Офлайн-карты Windows", den: "Windows offline maps",
		rec: true, pkgs: []string{"Microsoft.WindowsMaps"}},
	{id: "powerautomate", group: "tools", ru: "Power Automate", en: "Power Automate", dru: "Автоматизация для бизнеса", den: "Business automation",
		rec: true, pkgs: []string{"Microsoft.PowerAutomateDesktop"}},
	{id: "devhome", group: "tools", ru: "Dev Home", en: "Dev Home", dru: "Панель разработчика (снята с поддержки)", den: "Developer dashboard (deprecated)",
		rec: true, pkgs: []string{"Microsoft.Windows.DevHome"}},
	{id: "family", group: "tools", ru: "Microsoft Family", en: "Microsoft Family", dru: "Семейная безопасность", den: "Family safety",
		rec: true, pkgs: []string{"MicrosoftCorporationII.MicrosoftFamily"}},
	{id: "wallet", group: "tools", ru: "Кошелёк", en: "Wallet", dru: "Устаревшее приложение платежей", den: "Legacy payments app",
		rec: true, pkgs: []string{"Microsoft.Wallet"}},
	{id: "alarms", group: "tools", ru: "Часы и будильники", en: "Clock & Alarms", dru: "Таймеры, будильник, фокусировка", den: "Timers, alarms, focus sessions",
		pkgs: []string{"Microsoft.WindowsAlarms"}},
	{id: "sticky", group: "tools", ru: "Записки", en: "Sticky Notes", dru: "Заметки на рабочем столе", den: "Desktop notes",
		pkgs: []string{"Microsoft.MicrosoftStickyNotes"}},
	{id: "quickassist", group: "tools", ru: "Быстрая помощь", en: "Quick Assist", dru: "Удалённая помощь через интернет", den: "Remote help over the internet",
		pkgs: []string{"MicrosoftCorporationII.QuickAssist"}},
	{id: "journal", group: "tools", ru: "Журнал (рукописный)", en: "Journal (ink)", dru: "Рукописные заметки", den: "Handwritten notes",
		rec: true, pkgs: []string{"Microsoft.MicrosoftJournal"}},

	// Системное
	{id: oneDriveID, group: "system", ru: "OneDrive", en: "OneDrive", dru: "Облачная синхронизация файлов", den: "Cloud file sync",
		cru: "Файлы в облаке сохранятся, локальная синхронизация прекратится", cen: "Cloud files stay, local sync stops"},
}

func (d appDef) app() App {
	return App{
		ID: d.id, Group: d.group,
		Name: i18n.Pick(d.ru, d.en), Desc: i18n.Pick(d.dru, d.den),
		Caution: i18n.Pick(d.cru, d.cen), Recommended: d.rec, Packages: d.pkgs,
	}
}

func findApp(id string) (appDef, bool) {
	for _, d := range appDefs {
		if d.id == id {
			return d, true
		}
	}
	return appDef{}, false
}
