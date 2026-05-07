<div align="center">

<img src="https://github.com/openlibrecommunity/material/blob/master/olcrtc.png" width="250" height="250">

![License](https://img.shields.io/badge/license-WTFPL-0D1117?style=flat-square&logo=open-source-initiative&logoColor=green&labelColor=0D1117)
![Golang](https://img.shields.io/badge/-Golang-0D1117?style=flat-square&logo=go&logoColor=00A7D0)

</div>


# Краткий URI-формат для клиентов

Этот документ описывает **соглашение для разработчиков клиентских приложений**, которым нужен компактный способ передавать параметры подключения `olcrtc`.

Текущий `olcrtc` не парсит такой URI автоматически. Если клиентское приложение хочет использовать эту запись, оно должно само разобрать строку и передать полученные поля в свои вызовы `olcrtc`.

---

## Формат

```text
olcrtc://<Carrier>?<Transport>@<RoomID>#<EncryptionKey>%<ClientID>$<MIMO>
```

Все поля после `olcrtc://` считаются частью клиентского соглашения.

---

## Поля

| Поле | Значение |
|------|----------|
| `<Carrier>` | Имя carrier, например `telemost`, `jazz`, `wbstream` |
| `<Transport>` | Имя транспорта, например `datachannel`, `vp8channel`, `seichannel`, `videochannel` |
| `<RoomID>` | Идентификатор комнаты или carrier-specific room URL/ID |
| `<EncryptionKey>` | Ключ шифрования в hex, обычно 64 символа (`32` байта) |
| `<ClientID>` | Идентификатор клиента. Должен совпадать с ожидаемым значением на сервере |
| `<MIMO>` | Свободный комментарий для UI/метаданных, например `RU / olc free sub / IPv6` |

---

## Соответствие параметрам olcrtc

URI-поля сопоставляются с обычными параметрами так:

| URI поле | Параметр / значение |
|----------|---------------------|
| `<Carrier>` | `-carrier` |
| `<Transport>` | `-transport` |
| `<RoomID>` | `-id` |
| `<EncryptionKey>` | `-key` |
| `<ClientID>` | `-client-id` |
| `<MIMO>` | В `olcrtc` не передаётся. Это только клиентский комментарий |

`-link direct` и `-data data` в этом формате не кодируются, потому что для текущих сценариев они фиксированные.

---

## Разделители

Строка использует фиксированные разделители:

| Разделитель | После него идёт |
|-------------|-----------------|
| `://` | начало полезной нагрузки после схемы `olcrtc` |
| `?` | `<Transport>` |
| `@` | `<RoomID>` |
| `#` | `<EncryptionKey>` |
| `%` | `<ClientID>` |
| `$` | `<MIMO>` |

Рекомендуется не использовать эти символы внутри самих полей. Если клиенту это нужно, он должен ввести собственное escaping/percent-encoding правило и применять его симметрично при кодировании и декодировании.

---

## Примеры

### Полный пример

```text
olcrtc://wbstream?seichannel@room-01#d823fa01cb3e0609b67322f7cf984c4ee2e4ce2e294936fc24ef38c9e59f4799%android-01$RU / olc free sub / IPv6
```

Разбор:

| Поле | Значение |
|------|----------|
| `Carrier` | `wbstream` |
| `Transport` | `seichannel` |
| `RoomID` | `room-01` |
| `EncryptionKey` | `d823fa01cb3e0609b67322f7cf984c4ee2e4ce2e294936fc24ef38c9e59f4799` |
| `ClientID` | `android-01` |
| `MIMO` | `RU / olc free sub / IPv6` |

### Эквивалент CLI

```sh
./olcrtc -mode cnc \
  -carrier wbstream \
  -transport seichannel \
  -id room-01 \
  -client-id android-01 \
  -key d823fa01cb3e0609b67322f7cf984c4ee2e4ce2e294936fc24ef38c9e59f4799 \
  -link direct \
  -data data
```

---

## Короткие алиасы

Как хотите но лично я был бы против.
