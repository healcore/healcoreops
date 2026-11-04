# Code signing policy / Политика подписи кода

**Free code signing provided by [SignPath.io](https://about.signpath.io/), certificate by [SignPath Foundation](https://signpath.org/).**
Бесплатная подпись кода — [SignPath.io](https://about.signpath.io/), сертификат — [SignPath Foundation](https://signpath.org/).

> Status: the application to SignPath Foundation is pending. Until it is approved, releases
> are not signed; signed releases will be marked in the release notes.
> Статус: заявка в SignPath Foundation подана и ожидает рассмотрения. До одобрения релизы
> не подписаны; подписанные релизы будут отмечены в описании релиза.

## What is signed / Что подписывается

Only `HealCoreOps.exe` and `HealCoreOps-Setup.exe` built by GitHub Actions from this
repository ([`.github/workflows/build.yml`](../.github/workflows/build.yml)) on release tags `v*`.
Every signing request is approved manually.

Подписываются только `HealCoreOps.exe` и `HealCoreOps-Setup.exe`, собранные GitHub Actions из этого
репозитория по тегам релизов `v*`. Каждый запрос на подпись одобряется вручную.

## Team roles / Роли

| Role / Роль | Members / Участники |
|---|---|
| Committers and reviewers / Авторы и рецензенты | [@healcore](https://github.com/healcore) |
| Approvers / Одобряют подпись | [@healcore](https://github.com/healcore) |

Changes from outside contributors are reviewed before merging.
Изменения от сторонних участников проходят проверку перед слиянием.

## Privacy / Приватность

This program will not transfer any information to other networked systems unless
specifically requested by the user or the person installing or operating it.

Программа не передаёт никаких сведений в другие сетевые системы, если этого
специально не запросил пользователь или администратор.

Network access, in detail / Сетевые обращения подробно:

| When / Когда | Where / Куда | What / Что |
|---|---|---|
| Once a day, can be turned off in Settings or by policy `DisableUpdateCheck` / Раз в сутки, отключается в настройках или политикой | `api.github.com` | Latest release version (no data about the PC) / Номер последней версии (без сведений о ПК) |
| Drivers → Check for updates / Драйверы → Проверить | `*.nvidia.com` | GPU model and OS for the driver lookup / Модель видеокарты и ОС для подбора драйвера |
| Drivers / Windows Update pages | Microsoft Windows Update (system agent) | Standard Windows Update search / Обычный поиск Windows Update |
| Network page actions / Действия на странице «Сеть» | Hosts entered by the user; `1.1.1.1`, `8.8.8.8`, `9.9.9.9`, `www.microsoft.com` for the internet check | Ping, traceroute, port check, DNS |
| Network → Connections | DNS resolver configured in Windows | Reverse DNS of remote addresses / Обратный DNS удалённых адресов |
| Programs → Updates | winget sources | Standard winget queries / Обычные запросы winget |

No telemetry, analytics or crash reports are sent. Reports, inventory and support
bundles are saved locally and are sent only if the user does so.

Никакой телеметрии, аналитики и отчётов о сбоях программа не отправляет. Отчёты,
инвентаризация и пакеты для поддержки сохраняются локально и уходят куда-либо
только если их отправит сам пользователь.

## System changes / Изменения системы

Every change is shown with a warning before it is made, written to the app history
and the Windows event log, and can be undone where Windows allows it. The installer
provides an uninstaller (also silent: `uninstall.exe /uninstall /S`).

Каждое изменение сопровождается предупреждением, записывается в историю программы и
журнал Windows и, где это позволяет Windows, отменяется. Установщик ставит деинсталлятор
(в том числе тихий: `uninstall.exe /uninstall /S`).
