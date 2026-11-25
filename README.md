# Radspur

**Radspur by mwonline.at** – selbst gehostetes Tour-Tracking für Rad und Laufen, Routenplanung mit
GPX-Export für den Radcomputer und „Handy finden“. Android-App plus Server als Docker-Container, ohne
Cloud-Dienste Dritter. Mehrere Benutzer, Registrierung nur per Einladung.

- **App installieren:** [Anleitung](docs/app-installation.md) ·
  [neueste Version](https://github.com/Maggo787878/Radspur/releases/latest)
- **Kartendaten für die Routenplanung:** [hinzufügen und aktualisieren](docs/karten.md)

## Inhalt

- [Funktionen](#funktionen)
- [Aufbau](#aufbau)
- [Server einrichten](#server-einrichten)
- [Einstellungen](#einstellungen)
- [nginx davor](#nginx-davor)
- [Updates](#updates)
- [Benutzer verwalten](#benutzer-verwalten)
- [Backup und Wiederherstellung](#backup-und-wiederherstellung)
- [App-Releases](#app-releases)
- [Grenzen](#grenzen)
- [Entwicklung](#entwicklung)
- [API](#api)
- [Lizenzen](#lizenzen)

## Funktionen

### Android-App

- **Aufzeichnen** für **Rad** oder **Laufen** mit Live-Karte (OpenStreetMap), auch bei gesperrtem
  Bildschirm; Rad zeigt km/h, Laufen das Tempo in min/km
- Distanz, Zeit in Bewegung, Ø- und Höchstwert, Höhenmeter, GPS-Genauigkeit; Pause/Weiter
- **Touren**: Liste mit Filter, Detail mit Karte, GPX teilen; automatischer Upload zum Server
- **Statistik**: Woche, Monat, Jahr, gesamt, Diagramm 12 Wochen/Monate, Durchschnitte, Rekorde,
  **Excel-Export**
- **Handy finden**: meldet alle 15 Minuten Position und Akkustand und führt Befehle aus der Weboberfläche aus
- Anmeldung mit Benutzername und Passwort; pro Handy ein eigenes Token

### Weboberfläche

| Reiter | Inhalt |
|---|---|
| **Handy** | letzte Position, Genauigkeit, Akku, Verlauf (6 h / 24 h / 7 Tage); **Klingeln lassen** (volle Lautstärke über den Alarmkanal, auch im Lautlos-Modus) und **Position anfordern**; danach meldet sich das Handy 15 Minuten lang jede Minute |
| **Touren** | Touren mit Karte, GPX-Download, umbenennen, Sportart ändern, löschen; **Touren nachtragen** per GPX-Upload (z. B. vom Radcomputer, mehrere Dateien auf einmal, Doppelte werden erkannt) oder von Hand |
| **Routen** | **Routenplanung**: Orte suchen oder auf die Karte klicken, Punkte ziehen und einfügen; Profile Trekking, Rennrad, Gravel, MTB, „Sicher“, zu Fuß; Distanz, Anstieg, Abstieg, Zeit, Höhenprofil; speichern und als **GPX für den Radcomputer** herunterladen |
| **Statistik** | wie in der App, aus allen Touren am Server (inkl. nachgetragener), mit Diagramm und **Excel-Download** |
| **Konto** | Passwort ändern, angemeldete Handys ansehen und entfernen |
| **Benutzer** | nur Admin: einladen, sperren, Passwort-Link, löschen |

Jeder Benutzer sieht ausschließlich seine eigenen Daten. Auch der Admin sieht keine Positionen, Touren
oder Routen anderer.

### Sicherheit

- Passwörter mit bcrypt; Sitzungen, Geräte-Tokens und Einladungslinks nur als SHA-256-Hash gespeichert
- Sitzungen serverseitig: Abmelden, Sperren und Passwortänderung wirken sofort
- Sperre nach 5 Fehlversuchen für 15 Minuten – pro IP **und** pro Benutzername –, global höchstens etwa
  ein Fehlversuch pro Sekunde
- Content-Security-Policy, HSTS (bei HTTPS), CSRF-Schutz, keine fremden Skripte (Leaflet eingebettet)
- Container ohne Root-Rechte; BRouter nur intern erreichbar
- Strikte Datentrennung zwischen Benutzern, durch automatische Tests abgesichert

## Aufbau

```
Android-App ──HTTPS──┐
                     ▼
Browser ──HTTPS──► nginx ──HTTP──► radspur (Go, SQLite) ──► brouter (Routenberechnung, intern)
                                        └──► nominatim.openstreetmap.org (Ortssuche)
```

```
android/   Android-App (Kotlin, Jetpack Compose, osmdroid)
server/    Server (Go, SQLite, Weboberfläche mit Leaflet)
brouter/   Container für die Routenberechnung (BRouter)
deploy/    docker-compose.yml, .env.example, nginx-Vorlage
docs/      Anleitungen (App-Installation, Kartendaten)
.github/   CI: Server-Image, BRouter-Image, signierte APK
```

Die Images werden bei jedem Push von GitHub Actions gebaut und liegen öffentlich unter
`ghcr.io/maggo787878/radspur-server` und `ghcr.io/maggo787878/radspur-brouter` (amd64 und arm64).

## Server einrichten

Voraussetzungen: Docker mit Compose-Plugin, ein Reverse Proxy mit TLS (z. B. nginx oder Nginx Proxy
Manager), etwa **1,5 GB RAM** und **1 GB Speicher** für die Kartendaten (Österreich).

1. Repository holen:

   ```bash
   git clone https://github.com/Maggo787878/Radspur.git && cd Radspur/deploy
   ```

2. Einstellungen anlegen und anpassen – mindestens `ADMIN_PASSWORD` und `PUBLIC_URL`:

   ```bash
   cp .env.example .env && nano .env
   ```

3. Starten:

   ```bash
   docker compose pull && docker compose up -d
   ```

4. Beim ersten Start lädt BRouter die Kartendaten (rund 600 MB, einige Minuten):

   ```bash
   docker logs -f radspur-brouter
   ```

5. Reverse Proxy einrichten ([nginx davor](#nginx-davor)), dann im Browser mit Benutzername `admin` (bzw.
   `ADMIN_USER`) und `ADMIN_PASSWORD` anmelden.

`ADMIN_USER` und `ADMIN_PASSWORD` werden **nur beim ersten Start** verwendet, um das Admin-Konto
anzulegen. Danach das Passwort in der Weboberfläche unter **Konto** ändern.

## Einstellungen

In `deploy/.env`; nach Änderungen `docker compose up -d`.

| Variable | Standard | Bedeutung |
|---|---|---|
| `ADMIN_USER` | `admin` | Name des Admin-Kontos – nur beim ersten Start |
| `ADMIN_PASSWORD` | – | Passwort des Admin-Kontos (mind. 8 Zeichen) – nur beim ersten Start |
| `PUBLIC_URL` | aus der Anfrage | öffentliche Adresse, Basis für Einladungslinks, z. B. `https://radspur.example.at` |
| `LOCATION_RETENTION_DAYS` | `30` | Aufbewahrung des Positionsverlaufs; Touren und Routen bleiben immer |
| `RADSPUR_BIND` | `127.0.0.1` | Adresse für Port 8080: `127.0.0.1` = nur dieser Host, `0.0.0.0` = auch aus dem Netz |
| `RADSPUR_PORT` | `8080` | veröffentlichter Port |
| `SECURE_COOKIE` | `auto` | Secure-Flag am Login-Cookie: `auto` = bei HTTPS (über `X-Forwarded-Proto`), `true`, `false` |
| `BROUTER_SEGMENTS` | `E5_N45 E10_N45 E15_N45` | Kartenkacheln für die Routenplanung – [docs/karten.md](docs/karten.md) |
| `BROUTER_MEMORY` | `1g` | Arbeitsspeicher für BRouter |
| `BROUTER_REFRESH_DAYS` | `30` | Kartendaten beim Start neu laden, wenn älter |
| `PROXY_NETWORK` | `npm_default` | Docker-Netz des Proxys, nur mit `docker-compose.npm.yml` |
| `DEVICE_TOKEN` | leer | optional: gemeinsames Token aus Versionen vor der Benutzerverwaltung, gehört dem Admin |
| `RADSPUR_IMAGE`, `BROUTER_IMAGE` | `ghcr.io/maggo787878/…:latest` | Container-Images |
| `GEOCODER_URL` | `https://nominatim.openstreetmap.org` | Ortssuche; eigener Nominatim/Photon möglich |

## nginx davor

Der Container spricht HTTP auf Port 8080; TLS übernimmt der Reverse Proxy.

| nginx läuft … | `.env` | Ziel im Proxy |
|---|---|---|
| auf demselben Host | `RADSPUR_BIND=127.0.0.1` | `http://127.0.0.1:8080` |
| auf einem anderen Rechner | `RADSPUR_BIND=0.0.0.0` (Port 8080 in der Firewall von außen sperren) | `http://<docker-host>:8080` |
| als Container (z. B. Nginx Proxy Manager) | `PROXY_NETWORK=<netz>`, Start mit `docker compose -f docker-compose.yml -f docker-compose.npm.yml up -d` | `http://radspur:8080` |

Vorlage: [`deploy/nginx/radspur.conf`](deploy/nginx/radspur.conf). Wichtig sind:

- `client_max_body_size 32m;` – sonst scheitern Tour-Uploads und GPX-Importe über 1 MB mit `413`
- `proxy_set_header X-Forwarded-Proto $scheme;` – damit der Server HTTPS erkennt (Secure-Cookie, HSTS)
- `proxy_set_header X-Real-IP $remote_addr;` – echte Client-IP für Login-Sperre und Log
- **keinen** eigenen `Referrer-Policy`-Header setzen (z. B. `no-referrer`), sonst blockieren die
  OpenStreetMap-Kachelserver die Karte mit „403 Access blocked“

**Nginx Proxy Manager:** *Force SSL* aktivieren und unter *Advanced* `client_max_body_size 32m;` eintragen.

Über das Internet nur per **HTTPS** betreiben – Passwörter, Tokens und Positionen gehen sonst im
Klartext übers Netz.

## Updates

Im Ordner `deploy`:

```bash
git pull && docker compose pull && docker compose up -d
```

Datenbankänderungen neuer Versionen werden beim Start automatisch übernommen. Skripte und Stylesheets
tragen eine Versionskennung, der Browser lädt nach einem Update also automatisch die neuen Dateien.

**Ohne Git** (Dateien damals einzeln kopiert): die aktuelle `docker-compose.yml` im bisherigen Ordner
ersetzen – **nicht** in einen neuen Ordner wechseln, weil Docker die Daten-Volumes nach dem Ordnernamen
benennt:

```bash
cp docker-compose.yml docker-compose.yml.bak && curl -fsSLO https://raw.githubusercontent.com/Maggo787878/Radspur/main/deploy/docker-compose.yml
```

```bash
docker compose pull && docker compose up -d
```

Kartendaten aktualisieren: [docs/karten.md](docs/karten.md#kartendaten-aktualisieren).

## Benutzer verwalten

- **Einladen:** Weboberfläche → **Benutzer** → *Einladungslink erstellen*. Den Link (7 Tage gültig,
  einmal verwendbar) selbst per Mail oder Messenger schicken, dazu die
  [App-Anleitung](docs/app-installation.md).
- **Sperren** wirkt sofort für Browser und Handys; **Passwort-Link** erzeugt einen Link zum Setzen eines
  neuen Passworts; **Löschen** entfernt das Konto mit allen Daten.
- **Admin-Passwort vergessen:** Link direkt am Server erzeugen (entsperrt das Konto auch):

  ```bash
  docker exec radspur radspur-server reset-link admin
  ```

**Datenschutz:** Du speicherst Standortdaten anderer Personen. Sag den Eingeladenen, dass Positionen
`LOCATION_RETENTION_DAYS` Tage und Touren unbegrenzt gespeichert werden und dass der Admin sie nicht sieht.

## Backup und Wiederherstellung

Alle Daten (Benutzer, Touren, Routen, Positionen) liegen in **einer SQLite-Datei** im Volume
`<ordner>_radspur-data` (Name prüfen mit `docker volume ls | grep radspur-data`). Die Kartendaten im
Volume `…_brouter-segments` muss man nicht sichern – sie werden neu geladen.

**Sichern** (Radspur kurz anhalten, damit die Datenbank konsistent ist):

```bash
docker compose stop radspur && docker run --rm -v deploy_radspur-data:/data -v "$PWD":/backup alpine tar czf /backup/radspur-$(date +%F).tgz -C /data . ; docker compose start radspur
```

**Wiederherstellen** – Container entfernen, Volume neu anlegen und das Backup hineinentpacken
(die aktuelle Datenbank wird dabei ersetzt):

```bash
docker compose stop radspur && docker compose rm -f radspur && docker volume rm deploy_radspur-data
```

```bash
docker run --rm -v deploy_radspur-data:/data -v "$PWD":/backup alpine sh -c 'tar xzf /backup/radspur-JJJJ-MM-TT.tgz -C /data && chown -R 10001:10001 /data'
```

```bash
docker compose up -d radspur
```

`deploy_radspur-data` jeweils durch den Namen deines Volumes ersetzen.

## App-Releases

GitHub Actions baut bei jedem Push in `android/` eine APK. Ein Release mit Download-Link entsteht über
einen Versions-Tag – Ablauf in [docs/app-installation.md](docs/app-installation.md#neue-app-version-veröffentlichen).

Alle APKs müssen mit **demselben Schlüssel** signiert sein, sonst lassen sie sich nicht über die
installierte App installieren. Der Schlüssel liegt als Repository-Secret in GitHub:

| Secret | Inhalt |
|---|---|
| `RADSPUR_KEYSTORE_BASE64` | Keystore `radspur.jks` als Base64 (eine Zeile) |
| `RADSPUR_KEYSTORE_PASSWORD` | Keystore-Passwort |
| `RADSPUR_KEY_ALIAS` | `radspur` |
| `RADSPUR_KEY_PASSWORD` | Schlüssel-Passwort |

Der Workflow prüft nach jedem Build den SHA-256-Fingerabdruck des Zertifikats (`427be761…790905e32`) und
veröffentlicht kein Release, wenn er nicht passt. **Keystore und Passwort außerhalb von GitHub sichern** –
Secrets lassen sich nicht mehr auslesen.

## Grenzen

- Android führt Hintergrundjobs höchstens alle 15 Minuten aus, im Stromsparmodus seltener. „Klingeln“ und
  „Position anfordern“ wirken daher erst beim nächsten Check-in.
- Pro Benutzer wird ein Handy geortet; bei mehreren Handys zeigt die Weboberfläche die zuletzt gemeldete
  Position, und Befehle gehen an das Handy, das sich als nächstes meldet.
- Die Statistik in der **App** rechnet mit den Touren auf dem Handy; nachgetragene Touren erscheinen nur in
  der Statistik der Weboberfläche.
- Die App lädt Kartenkacheln online; ohne Mobilnetz bleibt die Karte unterwegs grau (die Aufzeichnung
  läuft weiter).
- Keine iOS-App; am iPhone funktioniert die Weboberfläche.

## Entwicklung

Server lokal (ohne Routenberechnung, bzw. mit `BROUTER_URL` auf einen laufenden BRouter):

```bash
cd server && ADMIN_PASSWORD=testtest DB_PATH=./dev.db SECURE_COOKIE=false go run .
```

Server-Tests – u. a. Datentrennung, Einladungen, Login-Sperre, Statistik, Routen, GPX-Import, Übernahme
alter Datenbanken:

```bash
cd server && go test ./...
```

App bauen:

```bash
cd android && ./gradlew assembleDebug
```

App-Tests – Statistik, Excel-Export und Screenshot-Tests der Bildschirme mit
[Paparazzi](https://github.com/cashapp/paparazzi) (Pixel 5 und kleines Gerät mit 130 % Schrift,
Referenzbilder in `android/app/src/test/snapshots/`):

```bash
cd android && ./gradlew verifyPaparazziDebug
```

Nach gewollten Änderungen an der Oberfläche die Referenzbilder neu erzeugen:

```bash
cd android && ./gradlew recordPaparazziDebug
```

## API

**App** (Bearer-Token aus `/api/device/login`):

| Methode | Pfad | Zweck |
|---|---|---|
| POST | `/api/device/login` | `{username, password, device}` → `{token}` |
| POST | `/api/device/locations` | Positionen melden; Antwort enthält offene Befehle |
| POST | `/api/device/commands/{id}/ack` | Befehl bestätigen |
| POST | `/api/device/tours` | Tour hochladen (Upsert per `clientId`, `type`: `bike` \| `run`) |

**Weboberfläche** (Session-Cookie; ändernde Anfragen mit Header `X-Radspur: 1`): `/api/web/status`,
`/locations`, `/commands`, `/tours` (inkl. `/tours/manual`, `/tours/import-gpx`, `PATCH /tours/{id}`),
`/stats`, `/stats.xlsx`, `/routes`, `/route-calc`, `/route-profiles`, `/geocode`, `/me`, `/admin/*`
(nur Admin). Einladungen: `GET/POST /api/invite/{token}`. GPX-Downloads: `/tours/{id}/gpx`,
`/routes/{id}/gpx`. Healthcheck: `GET /healthz`.

## Lizenzen

Kartendaten © [OpenStreetMap](https://www.openstreetmap.org/copyright)-Mitwirkende (ODbL) ·
[Leaflet](https://leafletjs.com) (BSD-2-Clause) · [osmdroid](https://github.com/osmdroid/osmdroid)
(Apache-2.0) · [BRouter](https://github.com/abrensch/brouter) (MIT) · Ortssuche
[Nominatim](https://nominatim.org)
