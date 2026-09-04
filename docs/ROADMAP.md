# transitforge — Proposed Architecture Roadmap

> Рабочий архитектурный план для обсуждения. Это **не замена** канонического `docs/ARCHITECTURE.md` до явного утверждения и переноса решений в него.

## 1. Цель продукта

`transitforge` должен стать понятной операторской панелью для управления цепочкой:

```text
Пользователь
    |
    v
RU Entry Node
    |
    | WireGuard primary / SSH TUN fallback
    v
Foreign Node
    |
    +--> 3x-ui
    |      `--> Xray inbounds
    |
    `--> SharX
           `--> Xray inbounds / SharX workers
```

Пользователь панели не должен вручную думать в терминах `iptables`, отдельных UUID ресурсов или десятков низкоуровневых форм.

Основной пользовательский сценарий:

```text
Добавить зарубежный сервер
    -> выбрать 3x-ui / SharX / уже установленную панель
    -> подключить или установить
    -> проверить доступность
    -> обнаружить inbounds
    -> выбрать, что публиковать через RU edge
    -> настроить туннель
    -> увидеть план
    -> проверить путь трафика
    -> применить
    -> постоянно видеть состояние маршрута
```

---

## 2. Главные принципы

1. **Сначала наблюдаемость, потом автоматизация.**
   Нельзя автоматически настраивать сервер, если после этого панель не умеет объяснить, где именно перестал проходить трафик.

2. **Не прятать проблемы за одним `HEALTHY/UNHEALTHY`.**
   Состояние маршрута состоит из нескольких независимых проверок.

3. **Не хранить SSH-пароли без необходимости.**
   Предпочтительный onboarding — одноразовый bootstrap-token и команда, которую оператор выполняет на новом сервере.

4. **3x-ui и SharX подключаются через provider adapters.**
   Бизнес-логика transitforge не должна зависеть от конкретных endpoint'ов конкретной панели.

5. **Никаких произвольных shell-команд из UI.**
   Установка поддерживаемых компонентов — только через versioned installer plans и allowlisted operations.

6. **Dry-run до любых сетевых изменений.**

7. **Русский и английский — равноправные языки UI с первого полноценного интерфейса.**

8. **UI показывает бизнес-сущности, а низкоуровневые детали доступны внутри detail/advanced views.**

---

## 3. Терминология UI

Чтобы интерфейс был понятным, предлагается разделить сущности.

### Entry Node

RU-side сервер — публичная точка входа пользователей.

Существующий `Node` в текущем transitforge.

Показывает:

- public IP;
- agent status;
- transport;
- WireGuard handshake;
- mappings;
- SNI routes;
- last reconcile;
- health.

### Foreign Node

Зарубежный сервер, куда в итоге должен прийти пользовательский трафик.

Содержит:

- public / management address;
- country / label;
- overlay IP;
- provider type;
- provider connection;
- backend-agent status;
- discovered inbounds;
- network diagnostics.

### Provider

ПО, управляющее Xray на Foreign Node.

Поддерживаемые provider types:

```text
3X_UI
SHARX
UNMANAGED
```

`UNMANAGED` означает, что transitforge знает только `overlay_ip:port`, но не управляет Xray-панелью.

### Published Route

Главная пользовательская сущность вместо необходимости вручную связывать mapping + tunnel + backend + SNI.

Пример:

```text
Public:
ru-edge-1.example.net:443 / TCP

via:
wg-ru1-cz1

to:
cz-01
SharX
VLESS Reality
10.200.90.2:443
```

Внутри Published Route transitforge всё ещё использует существующие:

- Tunnel
- PortMapping
- SniRoute
- Backend

но UI объединяет их в понятный маршрут.

---

## 4. Высокоуровневая архитектура

```text
                           +----------------------+
                           |      Web UI          |
                           |      RU / EN         |
                           +----------+-----------+
                                      |
                                      v
+------------------+       +----------+-----------+
| Human Auth       |       | transitforge Controller  |
| Users / RBAC/TOTP|------>| SQLite / API / Audit |
+------------------+       +----------+-----------+
                                      |
              +-----------------------+-----------------------+
              |                       |                       |
              v                       v                       v
      +---------------+       +---------------+       +---------------+
      | Entry Agent   |       | Provider      |       | Backend Agent |
      | RU node       |       | Adapters      |       | Foreign node  |
      +-------+-------+       +-------+-------+       +-------+-------+
              |                       |                       |
              |              +--------+--------+              |
              |              |                 |              |
              |              v                 v              |
              |           3x-ui              SharX            |
              |                                                 |
              +---------------- WireGuard ----------------------+
```

---

## 5. Provider Adapter Layer

Необходимо добавить абстракцию:

```text
ProviderAdapter
    Detect()
    Health()
    Version()
    ListInbounds()
    GetInbound()
    Stats()
    Capabilities()
```

Позже, отдельными capability:

```text
CreateInbound()
UpdateInbound()
DeleteInbound()
RestartCore()
```

На первом этапе **не надо пытаться унифицировать весь Xray JSON**.

Нужна общая минимальная модель:

```text
InboundSummary {
    provider_id
    name
    protocol
    transport
    listen
    port
    enabled
    rx_bytes
    tx_bytes
}
```

И поле provider-specific metadata для расширенных деталей.

### 5.1 3x-ui adapter

Подключение к уже установленному 3x-ui:

```text
base URL
Bearer API token
TLS verification options
```

Adapter должен уметь минимум:

- проверить API;
- определить версию;
- получить список inbounds;
- получить порты/protocol;
- получить traffic/status, если API версии это поддерживает.

Секрет API token хранится только через secret reference / encrypted secret storage.

### 5.2 SharX adapter

Режим 1 — SharX standalone panel на конкретном Foreign Node.

Подключение:

```text
panel URL
Bearer JWT/API token
TLS verification
```

Режим 2 — **позже**: один центральный SharX panel + несколько SharX worker nodes.

Это отдельный advanced mode, потому что у SharX уже есть собственная multi-node архитектура.

Не нужно на первом этапе дублировать её внутри transitforge.

### 5.3 Capability discovery

UI не должен предполагать, что обе панели умеют абсолютно одно и то же.

Adapter возвращает:

```text
Capabilities {
    list_inbounds
    traffic_stats
    create_inbound
    edit_inbound
    restart_core
    multi_node
    logs
}
```

UI показывает только реально поддерживаемые действия.

---

## 6. Foreign Node Agent

Одного API 3x-ui/SharX недостаточно для диагностики сети.

Нужен отдельный лёгкий компонент:

```text
transitforge-backend-agent
```

Он НЕ является копией текущего privileged edge-agent.

Основные задачи:

- heartbeat;
- local interface/route discovery;
- проверить, слушается ли конкретный TCP/UDP port;
- локально проверить доступность provider API;
- сообщить system/network metadata;
- выполнять allowlisted diagnostic probes;
- сообщать counters/результаты проверок;
- кратковременная packet metadata observation для диагностики.

Не давать ему Docker socket постоянно.

Не давать возможность выполнять arbitrary shell.

Для packet diagnostics capabilities выдаются минимально необходимые и только если оператор включает этот режим.

---

## 7. Добавление Foreign Node — интерактивный Wizard

Кнопка:

```text
+ Add foreign node
+ Добавить зарубежный сервер
```

### Step 1 — Server

Поля:

- Name
- Public IP / hostname
- Country / location
- Optional management address

### Step 2 — Software

Выбор:

```text
Install new:
  ○ 3x-ui
  ○ SharX

Connect existing:
  ○ Existing 3x-ui
  ○ Existing SharX

Advanced:
  ○ Unmanaged Xray/service
```

### Step 3 — Connection / Bootstrap

Предпочтительный режим новой установки:

Controller создаёт:

```text
one-time registration token
+
bootstrap command
```

UI показывает оператору одну команду.

Например концептуально:

```text
curl .../bootstrap.sh | sudo sh -s -- <ONE_TIME_TOKEN>
```

Bootstrap:

1. определяет ОС;
2. проверяет prerequisites;
3. устанавливает Docker при разрешении;
4. тянет pinned provider image/version;
5. создаёт secret/config files;
6. запускает provider;
7. запускает backend-agent;
8. регистрирует сервер;
9. инвалидирует one-time token.

**Не копировать реальные секреты в командную строку, если можно избежать этого.**

Лучше короткий одноразовый enrollment ID, который обменивается по HTTPS на bundle.

### Step 4 — Preflight

До установки/подключения:

```text
Controller -> Foreign management/API     OK
Foreign -> Internet/registry             OK
Foreign Docker                            OK
Required ports                            OK/WARN
Clock sync                                OK
Disk                                      OK
```

### Step 5 — Provider

После установки:

```text
Provider: SharX
Version: x.y.z
API: reachable
Xray: running
Inbounds: 4
```

### Step 6 — Select inbound

TransitForge получает реальные inbounds через provider adapter.

Оператор выбирает:

```text
VLESS Reality        TCP/443
Hysteria2            UDP/8443
VMess WS              TCP/2053
```

Не просим пользователя второй раз вручную вводить backend port, если он уже известен из панели.

### Step 7 — Entry Node

Выбрать RU Entry Node.

Панель предлагает:

- existing WG tunnel;
- create tunnel;
- public port;
- SNI hostname if relevant.

### Step 8 — Validate

Перед Apply показать end-to-end diagnostics.

### Step 9 — Plan

Показать:

```text
ADD tunnel ...
ADD mapping ...
ADD SNI route ...
```

### Step 10 — Apply

Только после review.

---

## 8. Диагностика пути трафика

Это один из ключевых функциональных блоков продукта.

Не использовать один общий статус.

Путь должен выглядеть как последовательность стадий:

```text
Client ingress
     |
     v
[1] RU public socket
     |
[2] Port mapping / SNI
     |
[3] WireGuard handshake
     |
[4] Overlay route
     |
[5] Edge -> Foreign overlay:port
     |
[6] Foreign host receives packet
     |
[7] Target port listening
     |
[8] Xray/provider inbound healthy
     |
[9] Response path
```

### Checks

#### Entry

- agent connected;
- public interface exists;
- expected listening/NAT state exists;
- mapping rule counter;
- SNI configuration state.

#### Tunnel

- WireGuard interface;
- latest handshake;
- RX/TX deltas;
- route to remote overlay;
- overlay ping when permitted.

#### Edge -> Foreign

- TCP connect to exact `overlay_ip:port`;
- UDP-specific probe when a protocol provides a meaningful response;
- latency;
- timeout classification.

#### Foreign

Backend agent checks:

- packet arrived at interface;
- socket is LISTENING for TCP;
- process/service owning port;
- provider says inbound enabled;
- provider/Xray health.

### Diagnostic evidence

Пример UI:

```text
RU edge received connection       PASS
Mapping counter increased         PASS
WireGuard handshake               PASS  18s ago
Overlay ping                      PASS  44ms
TCP 10.200.90.2:443               FAIL  timeout
Foreign node saw SYN              NO
```

Это уже позволяет предположить проблему между edge и foreign, а не в Xray.

Другой пример:

```text
Edge sent SYN                     PASS
Foreign saw SYN                   PASS
Port 443 listening                FAIL
```

Проблема локально на Foreign Node.

### Provider / ISP incident diagnostics

Для случаев, когда пакеты могут блокироваться провайдером, позднее нужен ещё один компонент:

```text
External Probe Runner
```

Один probe runner за пределами RU edge позволяет проверять:

```text
external Internet -> RU public IP:port
```

Несколько probe points из разных сетей позволят сравнивать доступность.

Это **Phase later**, а не первая версия.

---

## 9. Health Model

Не один boolean.

```text
NodeHealth {
    control_plane
    agent
    provider
    tunnel
    route
    service
    last_probe
}
```

Итог:

```text
HEALTHY
DEGRADED
UNREACHABLE
NOT_CONFIGURED
UNKNOWN
```

Причина всегда должна быть видна рядом.

Плохо:

```text
DEGRADED
```

Хорошо:

```text
DEGRADED
WireGuard is healthy, but foreign TCP/443 has timed out for 3 probes.
```

---

## 10. UI Information Architecture

Не раздувать sidebar десятком технических таблиц.

Предлагаемая структура:

```text
OVERVIEW
  Dashboard

INFRASTRUCTURE
  Entry nodes
  Foreign nodes
  Routes

MONITORING
  Path checks
  Events

ADMINISTRATION
  Users & Access
  API Reference
  Settings
```

Низкоуровневые ресурсы:

- tunnels;
- mappings;
- SNI routes;

остаются в системе, но в основном UI доступны как tabs/detail внутри Entry Node / Route.

Для продвинутого оператора можно иметь:

```text
Advanced
```

с прямым просмотром raw mappings/tunnels.

---

## 11. Dashboard

Dashboard не должен быть просто списком node cards.

Top summary:

```text
Entry nodes        3
Foreign nodes      5
Healthy routes    11
Degraded routes    1
Failed checks      2
```

Ниже:

### Routes requiring attention

```text
ru-01 -> cz-02 / VLESS Reality
DEGRADED
Foreign TCP/443 timeout
```

### Infrastructure

Entry/Foreign nodes compact table.

### Recent events

Последние значимые события.

Без фиктивных графиков.

---

## 12. Foreign Node Detail

Header:

```text
CZ-01
SharX 2.x
ONLINE
Prague
203.x.x.x
```

Tabs:

```text
Overview
Inbounds
Connectivity
Routes
Provider
Events
```

### Overview

- agent health;
- provider health/version;
- overlay;
- traffic;
- routes count.

### Inbounds

Discovered from 3x-ui/SharX.

### Connectivity

Все network checks.

### Provider

Provider-specific settings and link to native panel.

TransitForge не должен пытаться заменить весь 3x-ui/SharX UI.

---

## 13. Два языка: RU / EN

Это нужно делать **до** дальнейшего разрастания UI.

Правило:

Никаких строк интерфейса прямо в templates/handlers.

Использовать ключи:

```text
nav.dashboard
nav.entry_nodes
nav.foreign_nodes
node.agent_not_connected
route.check_failed
```

Embedded locales:

```text
internal/webui/i18n/en.json
internal/webui/i18n/ru.json
```

Locale priority:

```text
user preference
-> cookie/session preference
-> Accept-Language
-> English fallback
```

Переключатель языка в user menu:

```text
English
Русский
```

Preference хранится в профиле пользователя.

API возвращает stable error codes; UI локализует человекочитаемый текст.

Не переводить protocol names, IDs, raw diagnostics и plan lines.

---

## 14. Новые модели

Предлагается добавить поверх текущих моделей:

```text
ForeignNode {
    id
    name
    public_address
    management_address
    country
    overlay_addresses[]
    provider_type
    provider_connection_id
    backend_agent_id
    status
    labels
}

ProviderConnection {
    id
    foreign_node_id
    type              // 3X_UI | SHARX
    base_url
    auth_secret_ref
    tls_mode
    version
    last_health_at
    last_health_result
}

DiscoveredInbound {
    id
    foreign_node_id
    provider_type
    provider_external_id
    name
    protocol
    transport
    listen
    port
    enabled
    last_seen_at
}

PublishedRoute {
    id
    entry_node_id
    foreign_node_id
    inbound_ref
    tunnel_id
    mapping_id
    sni_route_id?
    desired_status
}

ProbeResult {
    id
    route_id
    check_type
    source
    target
    status
    latency
    evidence
    ts
}
```

Текущий `Backend` пока сохраняется как data-plane сущность.

Не делать destructive migration `Backend -> ForeignNode` одним шагом.

---

## 15. Установка 3x-ui / SharX

### Что НЕ делать

Не зашивать в controller:

```text
ssh root@host "curl random-upstream-script | bash"
```

без версии, проверки и понятного плана.

### Правильнее

Versioned installer definition:

```text
ProviderInstaller {
    provider
    supported_versions
    preflight
    required_ports
    image/source
    checksum/digest
    install_steps
    health_check
    rollback
}
```

Сначала transitforge поддерживает небольшой набор проверенных версий.

UI:

```text
SharX
Recommended version: ...
[Install]

3x-ui
Recommended version: ...
[Install]
```

Должна быть возможность:

```text
Use existing installation
```

Автоматическое обновление provider после выхода upstream версии **не включать**.

Update всегда отдельное human-reviewed действие.

---

## 16. SharX-specific design

SharX уже умеет:

- Docker-first deployment;
- API tokens / sessions;
- multi-node;
- worker pairing;
- worker health/status/stats/logs.

Поэтому два режима.

### Phase 1

SharX standalone на Foreign Node.

TransitForge работает с SharX Panel API через adapter.

### Later

SharX Multi-node integration.

```text
transitforge
   |
   `--> SharX central panel
           |
           +--> SharX worker CZ
           +--> SharX worker DE
           `--> SharX worker NL
```

В этом режиме transitforge не должен повторно "устанавливать SharX panel" на каждый worker.

---

## 17. 3x-ui-specific design

3x-ui adapter должен использовать его публичный panel API.

На первом этапе:

```text
health
version
inbound discovery
traffic/status
```

После стабилизации read-only integration можно добавить controlled CRUD inbounds.

Не начинать с полного remote CRUD.

---

## 18. Security

### Secrets

Хранить только encrypted secret references:

- 3x-ui API token;
- SharX JWT/token;
- backend enrollment secrets.

Никогда:

- HTML;
- audit detail;
- query string;
- console logs.

### Bootstrap token

- one-time;
- short TTL;
- target node bound;
- invalidated immediately after enrollment.

### Provider API

- TLS verify by default;
- explicit CA import for private PKI;
- `insecure_skip_verify` только как явно предупреждаемый lab mode.

### Installer

- pinned version/digest;
- no automatic upstream latest;
- rollback path;
- audit.

---

## 19. Что реализовывать по этапам

### Phase 0 — закончить текущий UI/Auth foundation

Сделать сейчас:

- human users;
- roles;
- password;
- TOTP;
- enterprise layout;
- API reference;
- **RU + EN i18n foundation**.

Не начинать Foreign Node installer одновременно с auth rewrite.

**Результат:** стабильная оболочка продукта.

---

### Phase 1 — Foreign Nodes + provider adapters READ ONLY

Добавить:

- Foreign Nodes;
- ProviderConnection;
- 3x-ui adapter;
- SharX adapter;
- Connect existing panel;
- health/version;
- list inbounds;
- foreign-node detail page.

Без автоматической установки.

**Результат:** transitforge уже видит реальные зарубежные панели и их inbounds.

---

### Phase 2 — Backend Agent + Path Diagnostics

Добавить:

- backend-agent;
- heartbeat;
- local port checks;
- network route checks;
- Edge -> Foreign TCP checks;
- WG diagnostics;
- evidence model;
- Connectivity tab;
- Path Checks page.

**Это нужно сделать до auto-install.**

**Результат:** когда трафик не идёт, панель объясняет, на каком участке проблема.

---

### Phase 3 — Route Wizard

Wizard:

```text
Foreign Node
-> discovered inbound
-> Entry Node
-> tunnel
-> public port/SNI
-> connectivity preflight
-> plan
-> audited dry-run
-> apply
```

Пользователь не создаёт вручную четыре независимых ресурса.

**Результат:** основная ценность продукта становится доступна через один понятный поток.

---

### Phase 4 — Automated Provider Bootstrap

Добавить:

- one-time enrollment;
- bootstrap endpoint;
- provider installer definitions;
- 3x-ui install;
- SharX standalone install;
- backend-agent install;
- installation progress;
- rollback on failed install.

Сначала поддержать Debian/Ubuntu.

Не делать сразу 8 distributions.

**Результат:** чистую VPS можно добавить почти одной командой.

---

### Phase 5 — SharX Multi-node

После стабильного standalone adapter:

- зарегистрировать SharX central panel;
- discover SharX workers;
- map worker -> ForeignNode;
- inbounds per worker;
- status/stats.

Не дублировать SharX worker management без необходимости.

---

### Phase 6 — External Probes

Добавить отдельные lightweight probe runners:

```text
RU ISP A
RU ISP B
EU
```

Они позволяют отвечать на вопрос:

> сервер реально недоступен или проблема только у конкретного провайдера/маршрута?

Показывать сравнение:

```text
Probe Moscow-A    FAIL
Probe Moscow-B    FAIL
Probe EU          PASS
```

Это уже сильный operational diagnostic layer.

---

## 20. Что пока НЕ делать

Чтобы не превратить панель в комбайн:

- billing;
- subscription sales;
- full client management from SharX/3x-ui;
- визуальный Xray JSON editor;
- автоматическое обновление provider;
- arbitrary SSH terminal in browser;
- packet capture UI с payload;
- Kubernetes;
- multi-region HA controller;
- custom RBAC editor;
- полноценную замену 3x-ui/SharX интерфейса;
- десятки графиков "для красоты".

---

## 21. Итоговый UX

Главный сценарий должен занимать несколько понятных действий:

```text
+ Add foreign node
        |
        +-- Install SharX
        +-- Install 3x-ui
        +-- Connect existing
                 |
                 v
           Connection OK
                 |
                 v
          Choose inbound
                 |
                 v
         Choose RU entry
                 |
                 v
       Validate traffic path
                 |
                 v
             Review plan
                 |
                 v
               Apply
```

После этого Dashboard показывает не технические внутренности, а результат:

```text
RU-01 -> CZ-01
VLESS Reality / TCP 443

HEALTHY
WG handshake: 12s
Edge -> backend: 42ms
Inbound: running
Last end-to-end check: 8s ago
```

Если проблема:

```text
DEGRADED

Public traffic reaches RU edge.
WireGuard is healthy.
Foreign node does not receive TCP/443 packets.

Likely failure area:
edge-to-foreign network path.
```

Это и должно быть основной ценностью панели.

---

> Ниже добавлены утверждённые дополнения по Docker-only deployment и UI/UX.

## 22. Docker-only Deployment Model

### 22.1 Жёсткое правило

На Foreign Node (и опционально на Entry Node) **единственный компонент, устанавливаемый вне контейнера — сам Docker Engine**. Всё остальное — provider panel, backend-agent, любые вспомогательные сервисы — только контейнеры, поднятые через `docker compose`.

Запрещено:

```text
apt install xray
apt install <panel>
любые systemd unit'ы для бизнес-компонентов
любые бинарники, скопированные напрямую в /usr/local/bin оператором вручную
```

Разрешено (и является единственным способом):

```text
docker.io / docker-ce (через официальный репозиторий Docker)
docker compose plugin
```

### 22.2 Bootstrap flow (детально)

```text
Оператор запускает:
curl -fsSL https://<controller>/bootstrap.sh | sudo sh -s -- <ENROLLMENT_ID>
```

Скрипт `bootstrap.sh` выполняет строго последовательные, идемпотентные шаги:

1. **OS detection** — парсинг `/etc/os-release` (`ID`, `VERSION_ID`).
   Поддерживаемые на Phase 4: `ubuntu` (20.04+), `debian` (11+).
   Остальные популярные дистры (`almalinux`, `rocky`, `fedora`) — фиксируются в support-matrix, но не блокируют запуск: скрипт выполняет best-effort установку Docker через тот же официальный convenience-путь и явно помечает статус `EXPERIMENTAL_OS` в отчёте preflight.

2. **Prerequisites check** — наличие `curl`, `systemd` (для docker.service), свободный диск, открытый исходящий 443 до registry.

3. **Docker install (если отсутствует)**:
   ```text
   - добавление официального GPG key + apt-репозитория Docker для конкретного дистрибутива
   - apt-get install docker-ce docker-ce-cli containerd.io docker-compose-plugin
   - systemctl enable --now docker
   ```
   Если Docker уже установлен — шаг пропускается, версия логируется в audit.

4. **Enrollment exchange** — скрипт не содержит секретов. Он обменивает `ENROLLMENT_ID` на bundle по HTTPS:
   ```text
   POST /api/v1/bootstrap/exchange
   { "enrollment_id": "..." }
   -> { provider_type, compose_bundle, pinned_images[], agent_token, ttl }
   ```

5. **Compose bundle deploy**:
   ```text
   /opt/transitforge/<foreign_node_id>/docker-compose.yml
   /opt/transitforge/<foreign_node_id>/.env      (0600, root-only)
   ```
   `docker compose pull` строго по digest (`@sha256:...`), затем `docker compose up -d`.

6. **Health verification** — bootstrap-скрипт ждёт до N секунд, пока:
   - контейнер panel не ответит на internal healthcheck,
   - backend-agent не отправит первый heartbeat в Controller.

7. **Регистрация** — Controller помечает Foreign Node как `PROVISIONED`, инвалидирует `ENROLLMENT_ID`.

8. **Отчёт оператору** — bootstrap-скрипт печатает итоговую таблицу preflight (см. 22.5) и код возврата 0/≠0.

Любой шаг, завершившийся ошибкой, откатывает предыдущий (`docker compose down`) и оставляет узел в статусе `PROVISION_FAILED` с сохранённым логом для повторной попытки — без ручной SSH-починки.

### 22.3 Состав compose-стека на Foreign Node

```yaml
# /opt/transitforge/<foreign_node_id>/docker-compose.yml (пример для 3x-ui)
services:
  panel:
    image: ghcr.io/rsnest/3xui:v2.4.1@sha256:<pinned-digest>
    restart: unless-stopped
    network_mode: host          # чтобы порты inbound'ов совпадали с реальными
    volumes:
      - panel_data:/etc/x-ui
    env_file: .env

  backend-agent:
    image: ghcr.io/rsnest/backend-agent:v0.9.0@sha256:<pinned-digest>
    restart: unless-stopped
    network_mode: host          # нужен для диагностики реальных сокетов хоста
    environment:
      - TRANSITFORGE_CONTROLLER_URL=${CONTROLLER_URL}
      - TRANSITFORGE_AGENT_TOKEN=${AGENT_TOKEN}
      - TRANSITFORGE_DIAG_MODE=basic   # basic | packet (включается оператором отдельно)
    # docker.sock НЕ пробрасывается сюда постоянно —
    # обновление/переустановка стека выполняется повторным запуском bootstrap,
    # а не через доступ агента к сокету.

volumes:
  panel_data:
```

Для SharX — тот же паттерн, отличается только `image:` и набор env-переменных panel-сервиса; ProviderInstaller definition хранит это как данные, а не как хардкод в bootstrap-скрипте.

### 22.4 ProviderInstaller как источник истины

```text
ProviderInstaller {
    provider: 3X_UI | SHARX
    supported_versions: [...]
    image_ref: "ghcr.io/rsnest/<provider>:<version>@sha256:<digest>"
    compose_template: <embedded yaml template>
    required_ports: [...]
    env_schema: [...]
    health_check: { path/cmd, timeout, retries }
    rollback: { previous_image_ref }
}
```

Controller генерирует `docker-compose.yml` и `.env` из этого definition на этапе enrollment exchange — bootstrap-скрипт их не редактирует и не содержит бизнес-логики версий.

### 22.5 Preflight-таблица (вывод bootstrap.sh)

```text
OS detected                 Ubuntu 22.04           OK
Docker                      installed (24.0.7)     OK
Outbound to registry        reachable               OK
Required ports (443,8443)   free                     OK
Disk space                  38 GB free               OK
Clock sync (NTP)            synced                   OK
Compose bundle pulled       3x-ui v2.4.1 (digest ok) OK
Panel healthcheck           passed (4s)              OK
Backend-agent heartbeat     received                 OK
```

### 22.6 Обновление и rollback

```text
Update = human-reviewed action в UI:
  [Update provider] -> показывает diff pinned digest старый/новый
  -> Apply -> docker compose pull <new-digest> && up -d
  -> health_check
       PASS -> статус обновлён
       FAIL -> automatic docker compose pull <previous-digest> && up -d (rollback)
```

Никакого `:latest`, никакого автообновления по расписанию.

### 22.7 Backend-agent и Docker socket

- На этапе установки бутстрап-скрипт сам управляет `docker compose`, у агента нет доступа к сокету.
- В runtime агенту **не пробрасывается** `/var/run/docker.sock`.
- Если в будущем понадобится агенту читать состояние контейнеров (не управлять ими) — это отдельная explicit capability `docker_introspect`, монтируемая read-only и включаемая оператором вручную, а не default.

---

## 23. UI/UX Design System

### 23.1 Референс и на что именно ориентируемся

Ориентир — паттерны enterprise security-console продуктов класса SIEM/VM (в духе MaxPatrol): **не копирование конкретных визуальных элементов Positive Technologies**, а заимствование архитектурных паттернов интерфейса, которые делают такие консоли понятными операторам:

- тёмная тема как основной режим (с возможностью светлой);
- узкая статичная левая навигация с иконками + подписями, сгруппированная по доменам (как в разделе 10 `arch.md`: OVERVIEW / INFRASTRUCTURE / MONITORING / ADMINISTRATION);
- список сущностей слева/сверху + **detail-панель справа** (master-detail), а не переход на отдельную полную страницу для каждой мелкой сущности;
- статус выражается **цветным чипом с коротким текстом причины**, а не голым индикатором (см. `HEALTHY / DEGRADED / UNREACHABLE` из раздела 9);
- глобальный поиск/фильтр по тегам вверху (страна, provider type, статус);
- панель событий/аудита как отдельная закреплённая область, а не всплывающие тосты, которые теряются;
- плотные, но не перегруженные таблицы: сортировка, но без десятков колонок по умолчанию — редко используемые поля прячутся в "Advanced".

### 23.2 Общий каркас экрана

```text
┌───────────┬─────────────────────────────────────────────────────────────┐
│           │  Top bar: global search · language switch (RU/EN) · user    │
│  Sidebar  ├─────────────────────────────────────────────────────────────┤
│ (icons +  │                                                             │
│  labels)  │   Main content (list / table / dashboard)                   │
│           │                                                             │
│ OVERVIEW  │   ┌───────────────────────────┬─────────────────────────┐   │
│ INFRA     │   │ Entity list (filterable)  │ Detail panel            │   │
│ MONITORING│   │                           │ tabs: Overview / ...    │   │
│ ADMIN     │   └───────────────────────────┴─────────────────────────┘   │
│           │                                                             │
└───────────┴─────────────────────────────────────────────────────────────┘
```

Detail-панель открывается **не модальным окном**, а как выезжающая справа боковая панель (side sheet) шириной ~40% экрана — так оператор не теряет контекст списка. Полноэкранный переход используется только для многошаговых wizard'ов (Add foreign node, Route Wizard).

### 23.3 Цвет и статус-система

Единая палитра статусов через весь продукт (совпадает семантически с Health Model из раздела 9):

```text
HEALTHY          зелёный
DEGRADED         жёлтый/оранжевый
UNREACHABLE      красный
NOT_CONFIGURED   серый (нейтральный, не тревожный)
UNKNOWN          серо-синий, с иконкой "нет данных"
```

Правило: **статус-чип никогда не показывается без причины рядом или по hover/click**. Пример в списке:

```text
● DEGRADED   Foreign TCP/443 timeout (3 probes)
```

Цвет — только индикатор, не единственный носитель смысла (текст обязателен) — это и accessibility-требование, и прямое продолжение принципа 2 из `arch.md` ("не прятать проблемы за одним статусом").

### 23.4 Дашборд (раздел 11 arch.md, в терминах виджетов)

```text
Row 1 — KPI-стрип (карточки-счётчики, кликабельные, ведут к отфильтрованному списку):
  Entry nodes | Foreign nodes | Healthy routes | Degraded routes | Failed checks

Row 2 — "Routes requiring attention" (таблица, сортировка по severity)

Row 3 — двухколоночная:
  слева — Infrastructure (компактная таблица Entry+Foreign nodes)
  справа — Recent events (лента, без графиков "для красоты")
```

Никаких decorative sparkline-графиков без функционального назначения — только там, где график несёт диагностическую ценность (latency over time на Connectivity-табе).

### 23.5 Route Wizard / Add Foreign Node — паттерн степпера

```text
[1 Server] → [2 Software] → [3 Bootstrap] → [4 Preflight] → [5 Provider]
   → [6 Inbound] → [7 Entry] → [8 Validate] → [9 Plan] → [10 Apply]
```

- Степпер закреплён сверху степ-шапки, шаги кликабельны только назад.
- Каждый шаг — своя карточка с кратким описанием "зачем этот шаг" (снижает когнитивную нагрузку для оператора, не обязанного знать WireGuard/Xray-термины).
- Step 9 (Plan) рендерится как diff-блок в моноширинном шрифте (`ADD tunnel ...`) — единственное место в UI, где допустим "сырой" технический текст на английском независимо от локали, согласно правилу из раздела 13 arch.md ("не переводить protocol names, IDs, raw diagnostics и plan lines").

### 23.6 Path Checks / Connectivity — визуализация стадий

Стадии из раздела 8 arch.md отображаются как **вертикальный чек-лист с PASS/FAIL/PENDING**, а не абстрактная диаграмма:

```text
[1] RU public socket            PASS
[2] Port mapping / SNI          PASS
[3] WireGuard handshake         PASS   18s ago
[4] Overlay route               PASS
[5] Edge → Foreign overlay:port FAIL   timeout
[6] Foreign host receives pkt   NO DATA
[7] Target port listening       NO DATA
[8] Provider inbound healthy    NO DATA
[9] Response path                NO DATA
```

Первый `FAIL` визуально выделяется (левая цветная полоса), всё, что ниже по цепочке и не может быть проверено из-за более ранней ошибки — статус `NO DATA`, а не ложный `FAIL`.

### 23.7 Информационная плотность и Advanced-режим

- Основные экраны показывают только бизнес-сущности (Entry Node, Foreign Node, Published Route) — низкоуровневые Tunnel/Mapping/SniRoute не выведены в основную навигацию (раздел 10 arch.md).
- В detail-панели каждой сущности — таб **Advanced**, где то же самое показано как raw-объекты с ID, JSON provider-metadata и т.п. Единый паттерн "прогрессивного раскрытия": по умолчанию просто, technical detail — один клик вглубь.

### 23.8 Локализация в UI-системе

- Переключатель RU/EN в user-меню (верхний правый угол), persist в профиле пользователя, согласно разделу 13.
- Ключи i18n покрывают весь chrome интерфейса (навигация, статусы, лейблы форм); protocol names, plan lines, raw diagnostic evidence — не переводятся.
- Направление письма LTR для обоих языков — доп. RTL-поддержка не требуется.

### 23.9 Компонентная система (для agent-исполнителя)

Рекомендуемый набор переиспользуемых UI-примитивов, покрывающих всё вышеописанное:

```text
StatusChip(status, reason?)
EntityListRow(icon, title, subtitle, statusChip, tags[])
DetailSidePanel(tabs[], header)
StepperWizard(steps[], currentStep, canGoBack)
DiffPlanBlock(lines[])            // моноширинный, ADD/REMOVE/CHANGE
CheckList(stages[])               // PASS/FAIL/PENDING/NO DATA
KpiCard(label, value, onClick)
EventFeedItem(severity, text, ts)
AdvancedTab(rawObjectJson)
```

Эти примитивы должны быть реализованы один раз и переиспользованы на всех экранах (Dashboard, Foreign Node Detail, Route Wizard, Path Checks) — не дублировать вёрстку под каждый экран отдельно.

---

## 24. Definition of Done для агента-исполнителя

- [ ] Bootstrap-скрипт ставит **только** Docker вне контейнеров; всё остальное — pinned images.
- [ ] ProviderInstaller definitions хранятся в Controller, не хардкожены в bootstrap-скрипте.
- [ ] backend-agent не имеет постоянного доступа к `docker.sock`.
- [ ] Update/rollback провайдера — только human-reviewed действие с diff pinned digest.
- [ ] UI построен на переиспользуемых примитивах из 23.9, а не point-in-time вёрстке под экран.
- [ ] Статус нигде не отображается без текстовой причины.
- [ ] RU/EN переключается без перезагрузки страницы, persist в профиле.
- [ ] Raw/технические детали доступны только через явный "Advanced"-таб, не засоряют основной экран.

---

## Appendix A. Upstream reconciliation — 3x-ui and SharX

Этот раздел уточняет Provider Adapter / ProviderInstaller после сверки с актуальными upstream-возможностями.
Если Appendix A противоречит более раннему умозрительному примеру, при реализации сначала нужно обновить соответствующий основной раздел и только затем писать код.

### A.1 3x-ui

#### Native multi-node and API

3x-ui рассматривается не только как standalone-панель. Адаптер должен динамически обнаруживать provider capabilities, включая native multi-node.

```text
Capabilities {
    list_inbounds
    traffic_stats
    create_inbound
    edit_inbound
    restart_core
    multi_node
    api_tokens
}
```

transitforge не должен дублировать native node-management 3x-ui. Если Foreign Node фактически является child node существующей 3x-ui installation, это должно отражаться как provider topology, а не как второй независимый control plane.

Upstream 3x-ui также поддерживает scoped / optionally expiring API tokens. Для повседневной интеграции Provider Adapter должен использовать отдельный API token с минимальным scope, а не admin password.

#### Unattended install

Upstream installer поддерживает:

```text
XUI_NONINTERACTIVE=1
```

и генерирует random initial credentials, записывая install result в:

```text
/etc/x-ui/install-result.env
```

Следовательно, transitforge не должен заранее придумывать постоянный admin password как часть собственного data model.

Предпочтительный flow:

```text
1. ProviderInstaller запускает pinned 3x-ui deployment.
2. Initial generated credentials читаются только на bootstrap stage.
3. Они используются для первичной настройки / создания scoped API token.
4. Scoped token сохраняется как encrypted secret reference.
5. Bootstrap больше не использует admin password для обычных API calls.
6. Любые временные credential values удаляются из памяти/temporary state как можно раньше.
```

`install-result.env` не должен попадать в audit, HTML, Controller logs или Git.

#### Node-token encryption

3x-ui поддерживает:

```text
NODE_TOKEN_ENCRYPTION = off | migration | required
```

Upstream default может отличаться, но **transitforge-managed installation policy** должна задавать:

```text
NODE_TOKEN_ENCRYPTION=required
```

как собственное security-hardening решение для новых managed deployments.

Ключ node-token encryption должен жить в persistent provider volume и входить в backup requirements.

#### Fail2ban capabilities

Upstream 3x-ui image включает Fail2ban-based IP-limit enforcement. При включённом Fail2ban контейнеру нужны соответствующие network capabilities.

ProviderInstaller должен генерировать capability conditionally:

```yaml
XUI_ENABLE_FAIL2BAN: true
```

→ container receives the required network capabilities.

```yaml
XUI_ENABLE_FAIL2BAN: false
```

→ лишние capabilities не выдаются.

Права контейнера не должны оставаться "на всякий случай".

#### Database

Для 3x-ui:

```text
default: SQLite
optional: PostgreSQL profile
```

PostgreSQL не поднимается автоматически на каждом Foreign Node. Это explicit deployment option.

#### Tunnel health

Если upstream 3x-ui имеет собственную automatic tunnel-health restart logic, transitforge-managed deployment не должен включать конфликтующие auto-remediation механизмы без явного решения.

Рекомендация:

```text
provider internal auto-restart: OFF by default
transitforge Path Checks: source of diagnosis
operator-reviewed remediation: explicit
```

Иначе provider может самостоятельно рестартовать Xray, а Controller увидит изменение постфактум без собственного plan/audit flow.

---

### A.2 SharX

#### Authentication

SharX поддерживает browser session authentication **и** отдельные Bearer JWT API tokens для automation/integration.

Поэтому для Provider Adapter не нужно эмулировать browser login как основной service-to-service механизм.

Предпочтительный flow:

```text
SharX panel
   -> create dedicated API token for transitforge integration
   -> token returned once
   -> transitforge stores encrypted secret reference
   -> Provider Adapter uses Bearer token
   -> token can be revoked independently
```

Cookie-session flow остаётся пользовательским/browser механизмом SharX, но не является нашим предпочтительным adapter auth.

#### PostgreSQL

SharX использует PostgreSQL как primary database в современном deployment.

Generated managed stack должен включать pinned PostgreSQL image и persistent DB volume, если выбран standalone SharX deployment.

Пример архитектуры:

```text
SharX panel container
        |
        v
PostgreSQL container
        |
 persistent volume
```

DB password хранится в `.env`/secret material с restrictive permissions и никогда не попадает в UI/logs.

#### Watchtower

SharX upstream поддерживает Watchtower / automatic update mechanisms, но transitforge-managed deployment **не включает Watchtower**.

Правило transitforge:

```text
new upstream image available
        |
        v
show update to operator
        |
        v
review old digest -> new digest
        |
        v
manual Apply
        |
   health verification
    /             \
 PASS             FAIL
  |                |
keep          rollback digest
```

Никакого production auto-update.

#### Installation path

Предпочтительный managed path — не запуск произвольного upstream shell installer с HEAD ветки.

Priority:

```text
A. Generate our controlled compose bundle from pinned, published images/digests.
B. Only if A is technically impossible, use a pinned upstream tag/commit installer
   after a separate review and checksum/version policy.
```

Если fallback installer используется, итоговый compose всё равно должен пройти policy normalization transitforge (например, исключение Watchtower).

#### ACME / port 80

Foreign Node в нашей архитектуре не обязан быть публичной точкой пользовательского входа.

Поэтому открытие TCP/80 ради Let's Encrypt на Foreign Node — explicit wizard option, а не default behavior.

В UI:

```text
Expose provider panel publicly for ACME?
[ ] Yes, expose HTTP challenge port 80
```

По умолчанию:

```text
No
```

В private-management варианте используются заранее предоставленный certificate / private CA / reverse proxy architecture согласно deployment policy.

#### Metrics

SharX Prometheus-compatible metrics могут быть дополнительным источником `ProviderHealth` и traffic observations.

Добавить capability:

```text
metrics_endpoint: true | false
```

Но metrics не заменяют Path Checks: provider metrics не доказывают, что пакет дошёл от конкретного Entry Node.

---

### A.3 Provider topology policy

Оба provider'а могут иметь собственную multi-node модель.

Поэтому Foreign Node и Provider Node не всегда 1:1.

Нужно поддержать:

```text
ProviderInstallation
    |
    +-- standalone
    |
    `-- native_multi_node
             |
             +-- provider node A
             +-- provider node B
             `-- provider node C
```

`ForeignNode` остаётся нашей network/route сущностью.

Provider adapter отвечает за mapping:

```text
ForeignNode <-> provider-specific node identity
```

transitforge не должен автоматически становиться вторым master-controller для provider-internal topology.

---

### A.4 Protocol and lifecycle model

Не делать закрытый enum, который требует migration на каждый новый Xray protocol.

Предпочтительно:

```text
protocol: stable normalized string
```

Известные значения первой версии:

```text
vless
vmess
trojan
shadowsocks
hysteria2
wireguard
mtproto
mixed
http_tunnel
unknown
```

И отдельно:

```text
lifecycle:
    xray
    sidecar
    external
```

`mtproto`/Telemt может иметь lifecycle=`sidecar`.

Path Checks для sidecar не должны требовать Xray-specific stages.

Пример:

```text
sidecar process healthy
port listening
Entry -> Foreign port reachable
packet/traffic observation
response
```

---

### A.5 Provider capability matrix

| Capability | 3x-ui | SharX | transitforge behavior |
|---|---|---|---|
| Inbound discovery | yes | yes | normalize via adapter |
| Traffic stats | yes | yes | normalize when available |
| Native multi-node | yes | yes | discover, do not duplicate |
| API auth for adapter | scoped/expiring token | Bearer API token | encrypted secret ref |
| Primary DB | SQLite; optional PostgreSQL | PostgreSQL | installer-specific |
| Metrics endpoint | discover dynamically | available | optional evidence source |
| Provider auto-update | not relied upon | upstream supports it | disabled in managed mode |
| MTProto / sidecar lifecycle | provider-dependent | Telemt | separate lifecycle checks |

Capabilities must be runtime-discovered/version-aware where possible. Do not hardcode a global forever-true matrix when provider versions can differ.

---

### A.6 Updated ProviderInstaller requirements

```text
ProviderInstaller {
    provider
    supported_versions
    image_refs[]             // pinned digest
    compose_template
    required_ports
    env_schema
    capability_policy
    credential_bootstrap
    health_check
    update_plan
    rollback
}
```

Installer must also declare:

```text
runtime_privileges
persistent_secret_files
persistent_data
backup_requirements
public_exposure_requirements
native_multi_node_support
```

Before installation UI must show a human-readable summary:

```text
Provider          3x-ui
Version           pinned version
Database          SQLite
Fail2ban          Enabled
Extra privileges  NET_ADMIN (+ provider-required capability)
Public ports      ...
Auto-update       Disabled
Rollback          Available
```

---

### A.7 Definition of Done additions

- [ ] 3x-ui adapter uses a dedicated scoped/expiring API token for normal calls; generated bootstrap admin credentials are not the daily integration credential.
- [ ] `NODE_TOKEN_ENCRYPTION=required` is an explicit transitforge managed-deployment policy.
- [ ] 3x-ui extra network capabilities are conditional on the enabled functionality that requires them.
- [ ] SharX adapter uses a dedicated API token rather than browser session emulation when the supported token API is available.
- [ ] SharX managed compose has no Watchtower service.
- [ ] SharX standalone deployment persists PostgreSQL data and includes DB backup requirements.
- [ ] Provider native multi-node is discovered and represented rather than duplicated by transitforge.
- [ ] Foreign port 80 exposure for ACME is explicit, never silent default.
- [ ] Provider image versions/digests are pinned.
- [ ] MTProto/Telemt sidecars use lifecycle-aware diagnostics rather than Xray-only checks.
- [ ] Provider metrics are evidence, not a replacement for end-to-end Path Checks.
