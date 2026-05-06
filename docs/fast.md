# Быстрый старт (через скрипты)

Этот способ самый простой. Тебе не нужен Go, не нужно ничего компилировать.
Скрипт всё сделает сам: скачает исходники, соберёт в контейнере, запустит.

Проект в бете. По проблемам: t.me/openlibrecommunity

---

## Что нужно установить

### git

```sh
apt install git # Debian / Ubuntu
pacman -S git # Arch
dnf install git # Fedora / RHEL / CentOS
```

### podman

Скрипт попробует установить podman сам, но лучше поставить заранее.

```sh
apt install podman # Debian / Ubuntu
pacman -S podman # Arch 
dnf install podman # Fedora / RHEL / CentOS
```

### curl (опционально, только для проверки)

```sh
apt install curl      # Debian/Ubuntu
pacman -S curl        # Arch
dnf install curl      # Fedora
```

---

## Шаг 1: Скачать репозиторий

```sh
git clone https://github.com/openlibrecommunity/olcrtc --recurse-submodules
cd olcrtc
```

---

## Шаг 2: Запустить сервер

На машине, через которую должен идти трафик (VPS, сервер за рубежом, домашний ПК):

```sh
./script/srv.sh
```

Скрипт задаст несколько вопросов. Отвечай Enter если устраивает значение по умолчанию.

### Carrier (на каком сервисе передавать трафик)

```
Select carrier:
  1) telemost
  2) jazz
  3) wbstream
Enter choice [1-3, default: 1]:
```

Выбери сервис. Смотри матрицу в [settings.md](settings.md) какой транспорт с каким carrier работает.

### Transport (как именно передавать данные)

```
Select transport:
  1) datachannel
  2) videochannel
  3) seichannel
  4) vp8channel
Enter choice [1-4, default: 1]:
```

Рекомендации:
- **datachannel** - работает везде помимо telemost, самый быстрый (~6 МБ/с)
- **vp8channel** - работает везде включая telemost, вводи FPS=60, batch=64
- **seichannel** - работает везде помимо telemost, вводи всё по умолчанию (FPS=20, batch=1, frag=900, ack-ms=3000)
- **videochannel** - работает везде включая telemost, медленный (~200 КБ/с), только в крайнем случае

### Room ID

```
Enter Room ID:
```

Для **telemost** - создай руму через сайт [телемоста](https://telemost.yandex.ru/) и вставь его.

Для **jazz** и **wbstream** можно нажать Enter - ID сгенерируется автоматически,
скрипт сам его вытащит из логов и покажет.

### Client ID

```
Enter Client ID [default: default]:
```

Это обязательный идентификатор клиента. Он должен быть одинаковым на сервере и клиенте.

### DNS

```
DNS server [default: 1.1.1.1:53]:
```

Нажми Enter. Менять не нужно если нет причин, на всякий можно поставить 77.88.8.8.

### SOCKS5 прокси для исходящего трафика

```
Use SOCKS5 proxy for egress? (y/N):
```

Если нет - просто Enter. Если хочешь чтобы сервер сам ходил через прокси - введи `y`.

### Параметры транспорта (только для vp8channel)

```
VP8 FPS [default: 25]: 60
VP8 batch size (frames per tick) [default: 1]: 64
```

Введи `60` и `64` - это оптимальные значения.

### Параметры транспорта (только для seichannel)

```
SEI FPS [default: 20]: 60
SEI batch size (frames per tick) [default: 1]: 64
SEI fragment size in bytes [default: 900]: 900
SEI ACK timeout in milliseconds [default: 3000]: 2000
```

Нажми Enter для всех - значения по умолчанию оптимальны.

### Результат

После запуска скрипт выведет:

```
[+] Server started successfully!

Container name: olcrtc-server
Carrier:        Carrier
Transport:      Transport
Room ID:        Room ID
Client ID:      default
Encryption key: Encryption key
```

**Сохрани Room ID, Client ID и Encryption key** - они нужны для клиента.

---

## Шаг 3: Запустить клиент

На своей машине (домашний ПК, ноутбук):

```sh
git clone https://github.com/openlibrecommunity/olcrtc --recurse-submodules
cd olcrtc
./script/cnc.sh
```

Отвечай на те же вопросы что на сервере - **carrier, transport, room ID и client ID должны совпадать**.

Когда спросит client ID:

```
Enter Client ID [default: default]: default
```

Введи тот же `client ID`, который использовал на сервере.

Когда спросит ключ:

```
Enter Encryption Key (hex): Encryption key
```

Вставь ключ с сервера.

### SOCKS5 адрес и порт

```
SOCKS5 ip [default: 127.0.0.1]:
SOCKS5 port [default: 8808]:
```

Нажми Enter оба раза. Прокси поднимется на `127.0.0.1:8808`.

### Результат

```
[+] Client started successfully!

Container name: olcrtc-client
Client ID:      default
SOCKS5 proxy:   127.0.0.1:8808
```

---

## Шаг 4: Проверить

```sh
curl --socks5-hostname 127.0.0.1:8808 https://icanhazip.com
```

Должен вернуть IP твоего сервера, а не домашний IP.

Или выставить переменную окружения чтобы всё шло через прокси:

```sh
export all_proxy=socks5h://127.0.0.1:8808
curl https://icanhazip.com
```

---

## Управление

### Логи

```sh
podman logs -f olcrtc-server   # на сервере
podman logs -f olcrtc-client   # на клиенте
```

### Остановить

```sh
podman stop olcrtc-server
podman stop olcrtc-client
```

### Перезапустить (просто запусти скрипт снова)

Скрипт сам останавливает старый контейнер перед стартом нового.

---

## Частые проблемы

**`podman: command not found`** - установи podman (см. начало документа).

**`git: command not found`** - установи git.
