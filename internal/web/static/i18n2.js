// Словарь разделов версии 1.8: ключ → [русский, английский]
Object.assign(DICT, {
  search_all: ["Поиск", "Search"],
  ng_monitor: ["Мониторинг", "Monitoring"], ng_clean: ["Очистка и ускорение", "Cleanup & speed"],
  ng_system: ["Система", "System"], ng_diag: ["Диагностика", "Diagnostics"],
  nav_disk: ["Место на диске", "Disk space"], nav_programs: ["Программы", "Programs"], nav_services: ["Службы и задачи", "Services & tasks"],
  nav_network: ["Сеть", "Network"], nav_crashes: ["Сбои и ошибки", "Crashes & errors"], nav_report: ["Отчёт и защита", "Report & security"],
  nav_history: ["История", "History"],
  managed: ["УПРАВЛЯЕТСЯ АДМИНИСТРАТОРОМ", "MANAGED BY ADMIN"],
  win_only: ["Раздел доступен только в Windows.", "This section is available on Windows only."],
  reboot_pending: ["Windows ждёт перезагрузки", "Windows is waiting for a reboot"],
  open_page: ["Открыть", "Open"], fix: ["Исправить", "Fix"], reveal: ["Показать", "Show"], open_btn: ["Открыть", "Open"],
  done_n: ["Готово: %1", "Done: %1"],

  // Проверка ПК
  checkup: ["Проверка компьютера", "PC checkup"],
  checkup_hint: ["Одна кнопка — проверка нагрузки, мусора, защиты, сбоев, драйверов, дисков и автозагрузки.", "One click checks load, junk, protection, crashes, drivers, drives and startup."],
  checkup_run: ["Проверить компьютер", "Check my PC"],
  checkup_running: ["Проверяем… это займёт 10–40 секунд", "Checking… this takes 10–40 seconds"],
  checkup_res: ["Проблем: %1 · предупреждений: %2 · проверено за %3 с", "Problems: %1 · warnings: %2 · checked in %3 s"],
  act_quickclean: ["Очистить мусор", "Clean up junk"], act_checkup: ["Проверить компьютер", "Check my PC"], act_theme: ["Сменить тему", "Toggle theme"],

  // Расписание
  schedule: ["Автоочистка по расписанию", "Scheduled cleanup"],
  schedule_on: ["Очищать мусор автоматически", "Clean up junk automatically"],
  schedule_when: ["Когда", "When"], schedule_what: ["Что очищать", "What to clean"],
  schedule_now: ["Выполнить сейчас", "Run now"],
  schedule_help: ["Работает, пока HealCoreOps запущен (в том числе свёрнутым в трей). Если компьютер был выключен в это время — очистка выполнится при следующем запуске в тот же день.",
    "Runs while HealCoreOps is running (including minimized to the tray). If the PC was off at that time, it runs on the next start the same day."],
  schedule_last: ["Последний раз: %1", "Last run: %1"], schedule_never: ["Ещё не выполнялась", "Never run yet"],
  sched_days: [["в воскресенье", "в понедельник", "во вторник", "в среду", "в четверг", "в пятницу", "в субботу", "каждый день"],
    ["on Sunday", "on Monday", "on Tuesday", "on Wednesday", "on Thursday", "on Friday", "on Saturday", "every day"]],
  sched_done: ["Очистка выполнена: освобождено %1", "Cleanup done: %1 freed"],
  sc_user_temp: ["Временные файлы", "Temp files"], sc_win_temp: ["Временные файлы Windows", "Windows temp"],
  sc_browser_cache: ["Кэш браузеров", "Browser caches"], sc_wer: ["Отчёты об ошибках", "Error reports"],
  sc_thumbs: ["Кэш эскизов", "Thumbnail cache"], sc_recycle: ["Корзина", "Recycle Bin"], sc_wu_download: ["Загрузки обновлений", "Update downloads"],

  // Диск
  junk: ["Мусор и кэши", "Junk & caches"], junk_total: ["Можно освободить: %1", "Can be freed: %1"],
  junk_clean_all: ["Очистить безопасное (%1)", "Clean safe items (%1)"], junk_empty: ["пусто", "empty"],
  junk_all_q: ["Очистить эти категории?\n\n%1\n\nУдаляются только кэши и временные файлы — программы создадут их заново. Корзина не трогается.",
    "Clean these categories?\n\n%1\n\nOnly caches and temp files are removed — apps recreate them. The Recycle Bin is not touched."],
  clean_btn: ["Очистить", "Clean"], cleanmgr: ["Очистка диска Windows", "Windows Disk Cleanup"], to_tweaks: ["К твикам", "Go to Tweaks"],
  clean_q: ["Очистить «%1» (%2)?", "Clean “%1” (%2)?"],
  clean_recycle_q: ["Очистить Корзину (%1)? Файлы из неё будут удалены навсегда.", "Empty the Recycle Bin (%1)? Its files will be deleted permanently."],
  cleaned: ["Освобождено %1", "Freed %1"],
  disk_analyze: ["Что занимает место", "What takes up space"], analyze: ["Анализ", "Analyze"],
  disk_intro: ["Выберите диск или папку и нажмите «Анализ» — покажем самые большие папки и файлы.", "Pick a drive or folder and click Analyze to see the largest folders and files."],
  disk_folders: ["Папки", "Folders"], disk_files: ["Самые большие файлы", "Largest files"],
  disk_scanning: ["Анализ… файлов: %1, объём: %2", "Analyzing… files: %1, size: %2"],
  disk_done: ["%1 в %2 файлах · %3 с", "%1 in %2 files · %3 s"], disk_skipped: [" · без доступа: %1 папок", " · no access: %1 folders"],
  disk_free: ["свободно %1 из %2", "%1 free of %2"],
  recycle: ["В Корзину", "To Recycle Bin"],
  recycle_q: ["Переместить в Корзину?\n\n%1\n\nЕго можно будет восстановить из Корзины или в «Истории».", "Move to the Recycle Bin?\n\n%1\n\nYou can restore it from the Recycle Bin or in History."],
  recycled: ["Перемещено в Корзину", "Moved to the Recycle Bin"], protected: ["системное", "system"],

  // Программы
  pr_installed: ["Установленные", "Installed"], pr_updates: ["Обновления", "Updates"],
  pr_search_ph: ["Поиск программы или издателя…", "Search programs or publishers…"],
  pr_sort: [["По названию", "По размеру", "По дате установки"], ["By name", "By size", "By install date"]],
  pr_stats: ["Программ: %1 · занимают около %2", "Programs: %1 · about %2"],
  pr_quiet_n: ["Удалить выбранные тихо (%1)", "Silently uninstall selected (%1)"],
  pr_uninstall: ["Удалить", "Uninstall"],
  pr_uninstall_q: ["Удалить «%1»?\n\nОткроется деинсталлятор программы — следуйте его указаниям. После удаления HealCoreOps предложит найти остатки.",
    "Uninstall “%1”?\n\nThe app's uninstaller will open — follow its instructions. Afterwards HealCoreOps will offer to find leftovers."],
  pr_uninstall_quiet_q: ["Тихо удалить %1 программ без окон?\n\n%2\n\nПрограммы удаляются по очереди; выполняйте, только если уверены.",
    "Silently uninstall %1 programs without windows?\n\n%2\n\nPrograms are removed one by one; do it only if you're sure."],
  pr_leftovers_title: ["Остатки «%1»", "Leftovers of “%1”"],
  pr_leftovers_hint: ["Найдены папки и ключи реестра, оставшиеся после удаления. Папки уйдут в Корзину, ключи — с резервной копией.",
    "Folders and registry keys left after uninstall. Folders go to the Recycle Bin, keys are backed up."],
  pr_leftovers_none: ["Остатков не найдено — программа удалена чисто.", "No leftovers found — the program was removed cleanly."],
  pr_leftovers_do: ["Удалить остатки", "Remove leftovers"], pr_leftovers_done: ["Удалено остатков: %1", "Leftovers removed: %1"],
  pr_all_users: ["для всех", "all users"], pr_user: ["для меня", "current user"], pr_silent: ["тихо", "silent"],
  pr_upd_title: ["Обновление программ через winget", "Update programs with winget"],
  pr_upd_help: ["Новые версии скачиваются из официальных источников издателей с проверкой контрольной суммы (Microsoft winget).", "New versions come from publishers' official sources with checksum verification (Microsoft winget)."],
  pr_upd_check: ["Проверить обновления", "Check for updates"], pr_upd_all: ["Обновить все", "Update all"], pr_upd_sel: ["Обновить выбранные (%1)", "Update selected (%1)"],
  pr_upd_none: ["Все программы обновлены.", "All programs are up to date."], pr_upd_found: ["Доступно обновлений: %1", "Updates available: %1"],
  pr_upd_intro: ["Нажмите «Проверить обновления» — займёт до минуты.", "Click “Check for updates” — it takes up to a minute."],

  // Службы и задачи
  sv_services: ["Службы", "Services"], sv_tasks: ["Планировщик заданий", "Task Scheduler"],
  sv_third: ["Только сторонние", "Third-party only"], sv_running: ["Только работающие", "Running only"],
  sv_stats: ["Показано %1 из %2 служб · работает %3", "Showing %1 of %2 services · %3 running"],
  sv_start: [{ auto: "Автоматически", delayed: "Автоматически (отложенный)", manual: "Вручную", disabled: "Отключена", boot: "При загрузке", system: "Системная" },
    { auto: "Automatic", delayed: "Automatic (delayed)", manual: "Manual", disabled: "Disabled", boot: "Boot", system: "System" }],
  sv_state: [{ running: "работает", stopped: "остановлена", paused: "приостановлена", starting: "запускается", stopping: "останавливается" },
    { running: "running", stopped: "stopped", paused: "paused", starting: "starting", stopping: "stopping" }],
  sv_critical: ["важная", "vital"], sv_start_btn: ["Запустить", "Start"], sv_stop_btn: ["Остановить", "Stop"], sv_restart_btn: ["Перезапустить", "Restart"],
  sv_need_admin: ["Просмотр доступен всем; чтобы запускать, останавливать и менять тип запуска, перезапустите программу от имени администратора.",
    "Anyone can view; to start, stop or change startup type, restart the app as administrator."],
  sv_stop_q: ["Остановить службу «%1»? Программы, которые от неё зависят, могут перестать работать.", "Stop service “%1”? Apps that depend on it may stop working."],
  tk_hide_ms: ["Скрыть задачи Microsoft", "Hide Microsoft tasks"], tk_stats: ["Показано %1 из %2 задач", "Showing %1 of %2 tasks"],
  tk_run: ["Запустить", "Run"], tk_never: ["не запускалась", "never ran"], tk_last: ["последний запуск %1", "last run %1"], tk_next: ["следующий %1", "next %1"],
  tk_trig: [{ 0: "по событию", 1: "однократно", 2: "ежедневно", 3: "еженедельно", 4: "ежемесячно", 5: "ежемесячно", 6: "при простое", 7: "при создании", 8: "при загрузке", 9: "при входе", 11: "при смене сеанса" },
    { 0: "on event", 1: "once", 2: "daily", 3: "weekly", 4: "monthly", 5: "monthly", 6: "on idle", 7: "on registration", 8: "at boot", 9: "at logon", 11: "on session change" }],
  tk_loading: ["Получение задач планировщика… до 30 секунд", "Loading scheduled tasks… up to 30 seconds"],

  // Сеть
  net_check: ["Проверка интернета", "Internet check"], net_check_hint: ["Проверим DNS и доступ к интернету.", "We'll check DNS and internet access."],
  net_check_run: ["Проверить интернет", "Check internet"], net_flush: ["Очистить кэш DNS", "Flush DNS cache"],
  net_tools: ["Инструменты", "Tools"], net_ping: ["Проверить связь", "Ping (TCP)"], net_port: ["Проверить порт", "Check port"], net_trace: ["Трассировка", "Traceroute"],
  net_tools_help: ["«Проверить связь» измеряет время подключения к узлу по TCP — работает даже там, где ping (ICMP) закрыт. Трассировка показывает путь до узла.",
    "“Ping (TCP)” measures TCP connect time — it works even where ICMP ping is blocked. Traceroute shows the route to the host."],
  net_adapters: ["Сетевые адаптеры", "Network adapters"],
  net_dns: ["DNS (поиск адресов)", "DNS (name lookup)"], net_inet: ["Доступ в интернет", "Internet access"],
  nv_ok: ["Интернет работает нормально.", "The internet works fine."],
  nv_dns: ["Интернет есть, но DNS не отвечает — сайты по имени не открываются. Смените DNS на Cloudflare или Google в «Обслуживании».", "Internet is up but DNS isn't answering — sites won't open by name. Switch DNS to Cloudflare or Google in Maintenance."],
  nv_offline: ["Нет подключения к интернету. Проверьте кабель или Wi-Fi и роутер; если не помогло — «Сбросить Winsock» в «Обслуживании».", "No internet connection. Check the cable/Wi-Fi and router; if that doesn't help, “Reset Winsock” in Maintenance."],
  nv_slow: ["Интернет работает, но медленно отвечает. Перезагрузите роутер; попробуйте другой DNS.", "The internet works but responds slowly. Restart the router; try another DNS."],
  ping_res: ["%1:%2 (%3) — %4 мс · потеряно %5 из %6 · DNS %7 мс", "%1:%2 (%3) — %4 ms · lost %5 of %6 · DNS %7 ms"],
  ping_fail: ["Узел не найден (DNS) или недоступен.", "Host not found (DNS) or unreachable."],
  port_open: ["Порт %1 на %2 открыт (%3 мс)", "Port %1 on %2 is open (%3 ms)"], port_closed: ["Порт %1 на %2 закрыт или недоступен", "Port %1 on %2 is closed or unreachable"],
  ms: ["мс", "ms"], gateway: ["Шлюз", "Gateway"], speed: ["Скорость", "Speed"],

  // Сбои
  cr_hint: ["Синие экраны, падения программ и частые ошибки Windows — с расшифровкой и советами.", "Blue screens, app crashes and frequent Windows errors — explained, with advice."],
  cr_days: [["7 дней", "30 дней", "90 дней"], ["7 days", "30 days", "90 days"]],
  cr_bsod: ["Синие экраны (BSOD)", "Blue screens (BSOD)"], cr_apps: ["Падения и зависания программ", "App crashes & hangs"],
  cr_unexp: ["Внезапные выключения", "Unexpected shutdowns"], cr_events: ["Частые ошибки в журналах Windows (7 дней)", "Frequent Windows log errors (7 days)"],
  cr_none_bsod: ["Синих экранов не было.", "No blue screens."], cr_none_apps: ["Программы не падали.", "No app crashes."],
  cr_none_unexp: ["Компьютер выключался правильно.", "The PC always shut down properly."], cr_none_events: ["Повторяющихся ошибок нет.", "No recurring errors."],
  cr_unexp_n: ["Выключений без завершения работы: %1 — пропадало питание, компьютер зависал или его выключали кнопкой.", "%1 times without proper shutdown — power loss, a hang or a forced power-off."],
  ev_crit: ["критическая", "critical"], ev_err: ["ошибка", "error"],
  cr_dump: ["дамп", "dump"], cr_event: ["журнал", "log"], cr_times: ["%1×", "%1×"], cr_hang: ["зависание", "hang"], cr_crash: ["падение", "crash"],
  cr_loading: ["Читаем журналы Windows… до минуты", "Reading Windows logs… up to a minute"],

  // Отчёт и защита
  rp_report: ["Отчёт о компьютере", "Computer report"],
  rp_report_help: ["Оборудование, драйверы, программы, автозагрузка, сбои, безопасность и последние изменения — в одном файле HTML (и JSON для автоматизации). Удобно отправить в техподдержку.",
    "Hardware, drivers, programs, startup, crashes, security and recent changes in one HTML file (plus JSON for automation). Handy to send to support."],
  rp_make: ["Создать отчёт", "Create report"], rp_folder: ["Папка отчётов", "Reports folder"],
  rp_saved: ["Отчёт сохранён: %1", "Report saved: %1"], rp_making: ["Собираем сведения… до минуты", "Collecting data… up to a minute"],
  rp_boot: ["Загрузка Windows", "Windows boot"], rp_security: ["Защита компьютера", "Security"],
  rp_admin: ["Для системного администратора", "For system administrators"],
  rp_admin_help: ["Командная строка и тихая установка — для развёртывания на многих компьютерах (GPO, Intune, SCCM, PDQ). Политики в HKLM ограничивают, что пользователи могут менять. Все изменения пишутся в журнал Windows «Приложение» (источник HealCoreOps) и в файлы журнала.",
    "Command line and silent install for mass deployment (GPO, Intune, SCCM, PDQ). HKLM policies restrict what users can change. Every change is written to the Windows Application log (source HealCoreOps) and to log files."],
  rp_policy_tpl: ["Шаблон политик (.reg)", "Policy template (.reg)"], rp_logs: ["Открыть журналы", "Open log files"],
  rp_tpl_saved: ["Шаблон сохранён: %1", "Template saved: %1"],
  pol_none: ["Политики не заданы — все функции доступны. Ключ политик: HKLM\\SOFTWARE\\Policies\\HealCoreOps", "No policies set — everything is available. Policy key: HKLM\\SOFTWARE\\Policies\\HealCoreOps"],
  pol_active: ["Действуют политики (%1): %2", "Policies in effect (%1): %2"],
  pol_blocked: ["Изменения в этом разделе запрещены администратором — доступен только просмотр.", "Changes in this section are blocked by your administrator — view only."],
  pol_names: [{ readOnly: "только просмотр", disableTweaks: "без твиков", disableDefender: "Defender не отключается", disableRegistry: "без исправления реестра", disablePrograms: "без удаления программ", disableServices: "без изменения служб", disableDisk: "без очистки диска", disableDrivers: "без установки драйверов", disableUpdateCheck: "без проверки обновлений", lockSettings: "настройки заблокированы" },
    { readOnly: "read-only", disableTweaks: "no tweaks", disableDefender: "Defender can't be disabled", disableRegistry: "no registry fixes", disablePrograms: "no program removal", disableServices: "no service changes", disableDisk: "no disk cleanup", disableDrivers: "no driver installs", disableUpdateCheck: "no update checks", lockSettings: "settings locked" }],
  cli_help: [`Командная строка (окно не открывается; коды возврата: 0 — успех, 2 — частично/есть проблемы, 3 — запрет политикой):
  HealCoreOps.exe --apply профиль.json         применить профиль (твики и правила)
  HealCoreOps.exe --report отчёт.html          отчёт о компьютере
  HealCoreOps.exe --checkup --json итог.json   проверка ПК
  HealCoreOps.exe --clean                      быстрая очистка
  HealCoreOps.exe --maintenance sfc            обслуживание (--list-maintenance)
  HealCoreOps.exe --tweak id=on                твик (--list-tweaks)

Тихая установка:
  HealCoreOps-Setup.exe /S [/ALLUSERS] [/NODESKTOP] [/LOG=setup.log] [/D=C:\\Путь]
  uninstall.exe /uninstall /S [/PURGE]`,
    `Command line (no window; exit codes: 0 success, 2 partial/problems, 3 blocked by policy):
  HealCoreOps.exe --apply profile.json         apply a profile (tweaks and rules)
  HealCoreOps.exe --report report.html         computer report
  HealCoreOps.exe --checkup --json result.json PC checkup
  HealCoreOps.exe --clean                      quick cleanup
  HealCoreOps.exe --maintenance sfc            maintenance (--list-maintenance)
  HealCoreOps.exe --tweak id=on                tweak (--list-tweaks)

Silent install:
  HealCoreOps-Setup.exe /S [/ALLUSERS] [/NODESKTOP] [/LOG=setup.log] [/D=C:\\Path]
  uninstall.exe /uninstall /S [/PURGE]`],
  sec_none: ["Проверки безопасности доступны в Windows.", "Security checks are available on Windows."],
  boot_last: ["Последняя загрузка: %1", "Last boot: %1"], boot_recent: ["Длительность последних загрузок", "Recent boot durations"],
  boot_slow: ["Что замедляет загрузку", "What slows down boot"], boot_none: ["Нет данных о замедлениях.", "No slowdown data."],
  boot_sec: ["%1 с", "%1 s"], boot_plus: ["+%1 с", "+%1 s"],
  boot_kind: [{ app: "программа", driver: "драйвер", service: "служба" }, { app: "app", driver: "driver", service: "service" }],
  as_boot: ["Скорость загрузки Windows", "Windows boot speed"],

  // История
  hi_changes: ["Изменения", "Changes"],
  hi_hint: ["Всё, что программа меняла в системе. Обратимые изменения можно отменить одной кнопкой.", "Everything the app changed in the system. Reversible changes can be undone in one click."],
  hi_clear: ["Очистить историю", "Clear history"], hi_clear_q: ["Очистить историю изменений? Отменить старые изменения отсюда будет нельзя.", "Clear the change history? You won't be able to undo old changes from here."],
  hi_undo: ["Отменить", "Undo"], hi_undo_q: ["Отменить изменение?\n\n%1", "Undo this change?\n\n%1"], hi_undone: ["отменено", "undone"],
  hi_empty: ["Изменений пока нет.", "No changes yet."], hi_all: ["Все разделы", "All sections"],
  area: [{ tweaks: "твики", apps: "приложения", autostart: "автозагрузка", services: "службы", tasks: "задачи", registry: "реестр", disk: "диск", programs: "программы", power: "питание", network: "сеть", maintenance: "обслуживание", profile: "профиль", drivers: "драйверы" },
    { tweaks: "tweaks", apps: "apps", autostart: "startup", services: "services", tasks: "tasks", registry: "registry", disk: "disk", programs: "programs", power: "power", network: "network", maintenance: "maintenance", profile: "profile", drivers: "drivers" }],
  src: [{ ui: "из окна", cli: "командная строка", schedule: "по расписанию", policy: "политика" }, { ui: "app", cli: "command line", schedule: "scheduled", policy: "policy" }],

  // Палитра
  pal_ph: ["Раздел, действие, твик или программа…", "Section, action, tweak or program…"],
  pal_foot: ["↑↓ — выбрать · Enter — выполнить · Esc — закрыть", "↑↓ select · Enter run · Esc close"],
  pal_page: ["раздел", "section"], pal_action: ["действие", "action"], pal_tweak: ["твик", "tweak"], pal_maint: ["обслуживание", "maintenance"], pal_prog: ["программа", "program"],
});
