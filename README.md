# Radspur

**Radspur by mwonline.at** – selbst gehostetes Tour-Tracking für Rad und Laufen plus „Handy finden“.
Android-App und ein kleiner Server als Docker-Container, ohne Cloud-Dienste Dritter.
Mehrere Benutzer, Registrierung nur per Einladung durch den Administrator.

**App herunterladen:** [neueste Version unter Releases](https://github.com/Maggo787878/Radspur/releases/latest)

```
android/   Android-App (Kotlin, Jetpack Compose, OpenStreetMap)
server/    Server (Go, SQLite, Weboberfläche mit Leaflet)
brouter/   Container für die Routenberechnung (BRouter)
deploy/    docker-compose.yml, .env.example, nginx-Vorlage
.github/   CI: baut das Server-Image (GHCR) und die signierte APK
```

## Funktionen

### App

**Aufzeichnen**
- Sportart **Rad** oder **Laufen** vor dem Start wählen
- Live-Karte (OpenStreetMap), GPS alle 1–2 s über einen Foreground-Service
- Rad: aktuelle Geschwindigkeit in km/h · Laufen: aktuelles Tempo in min/km
- Distanz, Zeit in Bewegung, Ø- und Höchstwert, Höhenmeter, GPS-Genauigkeit
- Pause/Weiter; die Aufzeichnung übersteht einen Neustart der App

**Touren**
- Liste mit Filter *Alle / Rad / Laufen*
- Detailansicht mit Karte und allen Werten, GPX teilen (`cycling` bzw. `running`)
- Fertige Touren werden automatisch zum Server hochgeladen

**Statistik**
- Kilometer, Anzahl und Zeit für *diese Woche*, *diesen Monat*, *dieses Jahr* und *gesamt*
- Balkendiagramm der letzten 12 Wochen (Kalenderwochen, Mo–So) bzw. 12 Monate
- Durchschnitt pro Woche und pro Tour, Rekorde (längste, schnellste, meiste Höhenmeter)
- Alles nach Sportart filterbar
- **Excel-Export** (`.xlsx`) mit den Blättern Übersicht, Wochen, Monate, Jahre und Touren –
  echte Datums-, Dauer- und Zahlenformate, km getrennt nach Rad und Laufen

**Handy finden**
- Meldet alle 15 min Position und Akkustand an den Server und führt Befehle aus

### Weboberfläche

- Letzte Position, Genauigkeit, Akku, Verlauf (6 h / 24 h / 7 Tage) auf der Karte
- **Klingeln lassen**: volle Lautstärke über den Alarm-Kanal, auch im Lautlos-Modus, 2 min oder bis Stopp
- **Position anfordern**: frischer GPS-Fix mit hoher Genauigkeit
- Nach einem Befehl meldet sich das Handy 15 min lang jede Minute („Suchmodus“)
- Touren mit Sportart ansehen, GPX herunterladen, löschen
- **Routen planen**: Orte suchen oder auf die Karte klicken, Punkte verschieben, Zwischenpunkte auf
  der Linie einfügen; Profile Trekking, Rennrad, Gravel, MTB, „Sicher“ und zu Fuß; Distanz, Anstieg,
  Abstieg, geschätzte Zeit und Höhenprofil; Routen speichern und als **GPX für den Radcomputer**
  herunterladen (Garmin, Wahoo, Sigma, Hammerhead …)
- **Statistik**: Woche, Monat, Jahr, gesamt; Diagramm der letzten 12 Wochen/Monate (Details beim
  Überfahren), Durchschnitte, Rekorde, Filter nach Sportart, **Excel-Download** – berechnet aus den
  hochgeladenen Touren, in der Zeitzone des Browsers
- Positionsverlauf wird nach `LOCATION_RETENTION_DAYS` gelöscht (Touren bleiben)
- **Konto**: Passwort ändern, angemeldete Handys ansehen und entfernen

### Benutzer

- Jeder Benutzer sieht und steuert nur **sein eigenes Handy und seine eigenen Touren**.
- Registrierung **nur per Einladung**: Der Admin erstellt im Reiter *Benutzer* einen Link
  (7 Tage gültig, einmal verwendbar) und schickt ihn selbst per Mail oder Messenger. Die eingeladene
  Person wählt Benutzername und Passwort.
- Der Admin verwaltet Konten – **sperren** (wirkt sofort für Browser und Handy), **Passwort-Link**
  erzeugen, **löschen** (mit allen Daten) – sieht aber bewusst **keine** Positionen oder Touren anderer.
- In der App meldet man sich mit Benutzername und Passwort an; der Server vergibt pro Handy ein
  eigenes Token. Das Passwort wird am Handy nicht gespeichert.

### Sicherheit

- Passwörter mit bcrypt; Sitzungen, Geräte-Tokens und Einladungslinks werden nur als SHA-256-Hash gespeichert
- Sitzungen serverseitig: Abmelden, Sperren und Passwortänderung wirken sofort
- Session-Cookie `HttpOnly`, `SameSite`, `Secure` bei HTTPS
- **Schutz gegen Passwort-Raten**: Sperre nach 5 Fehlversuchen für 15 min – pro IP **und** pro
  Benutzername –, zusätzlich global höchstens ~1 Fehlversuch pro Sekunde
- Strikte Datentrennung zwischen Benutzern, abgesichert durch automatische Tests
- Content-Security-Policy, HSTS (bei HTTPS), `X-Frame-Options`, `nosniff`, Permissions-Policy
- Alle Skripte vom eigenen Server (Leaflet eingebettet), keine fremden CDNs
- CSRF-Schutz für alle ändernden Web-Anfragen
- Container läuft ohne Root-Rechte

## Server betreiben

Der Container spricht HTTP auf Port 8080 und ist für den Betrieb hinter **nginx** gedacht.

```bash
git clone https://github.com/Maggo787878/Radspur.git && cd Radspur/deploy
```

```bash
cp .env.example .env && nano .env
```

In `.env` mindestens `ADMIN_PASSWORD` und `PUBLIC_URL` setzen, dann:

```bash
docker compose pull && docker compose up -d
```

**Update:** im Ordner `deploy`

```bash
git pull && docker compose pull && docker compose up -d
```

### Einstellungen (`.env`)

| Variable | Standard | Bedeutung |
|---|---|---|
| `ADMIN_USER` | `admin` | Name des Admin-Kontos – nur beim **ersten Start** verwendet |
| `ADMIN_PASSWORD` | – | Passwort des Admin-Kontos (mind. 8 Zeichen) – nur beim **ersten Start** verwendet, danach in der Weboberfläche ändern |
| `PUBLIC_URL` | aus der Anfrage | öffentliche Adresse, Basis für Einladungslinks (z. B. `https://radspur.example.at`) |
| `DEVICE_TOKEN` | leer | optional: gemeinsames Geräte-Token aus Versionen vor der Benutzerverwaltung, gehört dem Admin |
| `LOCATION_RETENTION_DAYS` | `30` | Aufbewahrung des Positionsverlaufs |
| `RADSPUR_BIND` | `127.0.0.1` | Adresse, auf der Port 8080 veröffentlicht wird (`0.0.0.0` = aus dem Netz erreichbar) |
| `RADSPUR_PORT` | `8080` | veröffentlichter Port |
| `PROXY_NETWORK` | `npm_default` | Docker-Netz des Proxys (nur mit `docker-compose.npm.yml`) |
| `SECURE_COOKIE` | `auto` | `auto` = Secure-Flag nur bei HTTPS, `true` = immer, `false` = nie |
| `BROUTER_SEGMENTS` | `E5_N45 E10_N45 E15_N45` | Kartenkacheln für die Routenplanung (je 5 × 5 Grad), Standard Österreich mit Grenzgebieten – siehe unten |
| `BROUTER_MEMORY` | `1g` | Arbeitsspeicher für BRouter |
| `RADSPUR_IMAGE` | `ghcr.io/maggo787878/radspur-server:latest` | Container-Image |

### nginx davor

| nginx läuft … | `.env` | Ziel im Proxy |
|---|---|---|
| auf demselben Host | `RADSPUR_BIND=127.0.0.1` | `http://127.0.0.1:8080` |
| auf einem anderen Rechner | `RADSPUR_BIND=0.0.0.0` | `http://<docker-host>:8080` |
| als Container (z. B. Nginx Proxy Manager) | `PROXY_NETWORK=<netz>`, Start mit `docker compose -f docker-compose.yml -f docker-compose.npm.yml up -d` | `http://radspur:8080` |

Eine fertige Vorlage liegt in `deploy/nginx/radspur.conf`. Wichtig sind darin:

- `client_max_body_size 32m;` – sonst lehnt nginx Tour-Uploads über 1 MB mit `413` ab
- `X-Forwarded-Proto $scheme` – damit der Server HTTPS erkennt (Secure-Cookie, HSTS)
- `X-Real-IP $remote_addr` – echte Client-IP für Login-Sperre und Log
- **keinen** eigenen `Referrer-Policy`-Header setzen (z. B. `no-referrer`), sonst blockieren die
  OpenStreetMap-Kachelserver die Karte mit „403 Access blocked“

**Nginx Proxy Manager:** *Force SSL* aktivieren und unter *Advanced* `client_max_body_size 32m;` eintragen.

Über das Internet sollte die Seite nur per **HTTPS** erreichbar sein (Let's Encrypt im Proxy).
Reines HTTP funktioniert, Passwort, Token und Positionen gehen dann aber im Klartext übers Netz –
das ist nur im LAN oder über VPN vertretbar. Ist `RADSPUR_BIND=0.0.0.0` gesetzt, Port 8080 in der
Firewall von außen sperren.

### Umstieg von der Einzelbenutzer-Version

Beim ersten Start der neuen Version legt der Server das Admin-Konto aus `ADMIN_USER` (Standard
`admin`) und dem bisherigen `ADMIN_PASSWORD` an und ordnet ihm **alle vorhandenen Touren und
Positionen** zu. Ein Handy mit dem alten `DEVICE_TOKEN` funktioniert weiter. Danach:

1. Im Browser mit Benutzername `admin` und dem bisherigen Passwort anmelden.
2. In der App unter *Einstellungen* abmelden und mit Benutzername und Passwort neu anmelden.
3. Wenn alle Handys umgestellt sind, `DEVICE_TOKEN` in `.env` leeren und den Container neu starten.

### Admin-Passwort vergessen

Ein Link zum Setzen eines neuen Passworts (7 Tage gültig) lässt sich direkt am Server erzeugen –
das entsperrt das Konto auch:

```bash
docker exec radspur radspur-server reset-link admin
```

### Routenplanung (BRouter)

Die Routen berechnet ein eigener Container mit [BRouter](https://github.com/abrensch/brouter), der nur
intern für den Radspur-Server erreichbar ist. Beim ersten Start lädt er die Kartendaten (OpenStreetMap,
~150–250 MB je Kachel) von brouter.de ins Volume `brouter-segments` und frischt sie alle 30 Tage auf –
der erste Start dauert daher einige Minuten (`docker logs -f radspur-brouter`).

Andere Regionen: Kacheln in `BROUTER_SEGMENTS` ergänzen (Namen = linke untere Ecke, z. B. `E10_N50`
für 10–15° Ost / 50–55° Nord; Übersicht unter https://brouter.de/brouter/segments4/), dann
`docker compose up -d`. Liegt ein Punkt außerhalb, meldet die Planung das.

Die Ortssuche nutzt den öffentlichen Dienst Nominatim von OpenStreetMap (höchstens eine Anfrage pro
Sekunde, Ergebnisse werden zwischengespeichert). Ein eigener Nominatim-/Photon-Server lässt sich mit
`GEOCODER_URL` eintragen.

**GPX auf den Radcomputer:** Die Datei ist ein GPX-1.1-Track mit Höhen. Garmin: per USB nach
`Garmin/NewFiles` kopieren oder über Garmin Connect *Strecken → Importieren*. Wahoo, Hammerhead, Sigma
u. a.: Import über die jeweilige App. Sehr lange Routen werden auf höchstens 9000 Punkte vereinfacht
(Abweichung ≤ 2 m), weil ältere Geräte längere Strecken abschneiden.

### Daten und Backup

Die Daten liegen im Docker-Volume `deploy_radspur-data` (SQLite). Backup:

```bash
docker run --rm -v deploy_radspur-data:/data -v "$PWD":/backup alpine tar czf /backup/radspur-data.tgz -C /data .
```

Datenbank-Änderungen neuer Versionen werden beim Start automatisch nachgezogen.

### Image aus GHCR

Jeder Push auf `main` mit Änderungen in `server/` baut `ghcr.io/maggo787878/radspur-server:latest`
für amd64 und arm64. Das Paket ist öffentlich, `docker compose pull` braucht keinen Login.

## App installieren

1. Auf dem Handy die [neueste Release-Seite](https://github.com/Maggo787878/Radspur/releases/latest)
   öffnen und `radspur.apk` herunterladen.
2. Datei antippen und installieren. Beim ersten Mal „Unbekannte Apps installieren“ für den Browser
   bzw. Dateimanager erlauben; eine Warnung von Play Protect mit *Trotzdem installieren* bestätigen.
3. Updates genauso: neue APK über die vorhandene App installieren – Touren und Einstellungen bleiben erhalten.

### Einrichtung

1. **Einstellungen**: Server-URL (`https://…`), Benutzername und Passwort eintragen → *Anmelden*.
   Kurz darauf erscheint „Letzte Meldung an Server“ und die Position in der Weboberfläche.
   Ein Konto gibt es nur per Einladung vom Administrator.
2. Berechtigungen erteilen: Standort, Benachrichtigungen, **Standort im Hintergrund („Immer zulassen“)**
   und **Akku-Optimierung deaktivieren** – sonst funktioniert „Handy finden“ nicht zuverlässig.
3. Samsung, Xiaomi, Huawei & Co.: Radspur zusätzlich in der Energiespar-/Autostart-Verwaltung des
   Herstellers auf „nicht einschränken“ stellen.

## Neue Version veröffentlichen

1. Änderungen nach `main` pushen – GitHub Actions baut Server-Image und APK und prüft die Signatur.
2. [Neues Release anlegen](https://github.com/Maggo787878/Radspur/releases/new): *Choose a tag* →
   neuen Tag eintippen (z. B. `v0.3.0`) → *Create new tag on publish* → **Publish release**.
3. Nach 3–4 Minuten hängt `radspur.apk` am Release. Der Versionsname der App kommt aus dem Tag
   (`v0.3.0` → `0.3.0`), die interne Versionsnummer ist die Laufnummer des Builds.

### Signatur

Alle APKs müssen mit **demselben Schlüssel** signiert sein, sonst lassen sie sich nicht über die
installierte App installieren. Der Schlüssel liegt als Repository-Secret in GitHub
(*Settings → Secrets and variables → Actions*):

| Secret | Inhalt |
|---|---|
| `RADSPUR_KEYSTORE_BASE64` | Keystore-Datei `radspur.jks` als Base64 (eine Zeile) |
| `RADSPUR_KEYSTORE_PASSWORD` | Keystore-Passwort |
| `RADSPUR_KEY_ALIAS` | `radspur` |
| `RADSPUR_KEY_PASSWORD` | Schlüssel-Passwort |

Der Workflow prüft nach jedem Build den SHA-256-Fingerabdruck des Zertifikats
(`427be761…790905e32`) und bricht ab, wenn er nicht passt. Ohne Secrets wird kein Release veröffentlicht.

**Keystore und Passwort unbedingt außerhalb von GitHub sichern** (z. B. Passwortmanager) –
Secrets lassen sich nicht mehr auslesen, und ohne Schlüssel sind keine Updates mehr möglich.

## Grenzen

- Android führt Hintergrundjobs frühestens alle 15 min aus, im Stromsparmodus auch seltener. Ein Befehl
  aus dem Web („Klingeln“, „Position“) wirkt daher erst beim nächsten Check-in. Danach reagiert das
  Handy 15 min lang minütlich. Mögliche Ausbaustufe: Push über einen selbst gehosteten **ntfy**-Server.
- Pro Benutzer wird ein Handy geortet – bei mehreren angemeldeten Handys zeigt die Weboberfläche die
  zuletzt gemeldete Position und Befehle gehen an das Handy, das sich als nächstes meldet.
- Die Statistik in der App rechnet mit den Touren auf dem Handy; auf ein neues Handy werden Touren
  (noch) nicht vom Server zurückgeholt.
- Ausgeschaltetes Handy oder Handy ohne Netz kann nicht gefunden werden – die zuletzt gemeldete
  Position bleibt sichtbar.

## API

| Methode | Pfad | Auth | Zweck |
|---|---|---|---|
| POST | `/api/device/login` | – | Anmeldung `{username, password, device}` → `{token}` (Geräte-Token) |
| POST | `/api/device/locations` | Bearer Geräte-Token | Positionen melden; Antwort enthält offene Befehle |
| POST | `/api/device/commands/{id}/ack` | Bearer | Befehl bestätigen |
| POST | `/api/device/tours` | Bearer | Tour hochladen (Upsert per `clientId`, `type`: `bike` \| `run`) |
| GET | `/healthz` | – | Healthcheck |

Die Web-API (`/api/web/*`) verlangt ein Session-Cookie nach Login und den Header `X-Radspur: 1`
für ändernde Anfragen; `/api/web/admin/*` zusätzlich die Rolle Admin. Einladungen:
`GET/POST /api/invite/{token}`, Seite `/einladung/{token}`.

## Entwicklung

Server lokal starten:

```bash
cd server && ADMIN_PASSWORD=testtest DB_PATH=./dev.db go run .
```

App bauen:

```bash
cd android && ./gradlew assembleDebug
```

Server-Tests (u. a. Datentrennung zwischen Benutzern, Einladungen, Sperren, Login-Sperre, Übernahme
einer alten Datenbank):

```bash
cd server && go test ./...
```

App: Unit-Tests (Statistik, Excel-Export) und Screenshot-Tests der Bildschirme mit
[Paparazzi](https://github.com/cashapp/paparazzi) – ohne Gerät oder Emulator, auf einem Pixel 5 und
einem kleinen Gerät mit 130 % Schriftgröße. Referenzbilder liegen in `android/app/src/test/snapshots/`.

```bash
cd android && ./gradlew verifyPaparazziDebug
```

Nach gewollten Änderungen an der Oberfläche die Referenzbilder neu erzeugen:

```bash
cd android && ./gradlew recordPaparazziDebug
```

## Lizenzen

Kartendaten © [OpenStreetMap](https://www.openstreetmap.org/copyright)-Mitwirkende ·
[Leaflet](https://leafletjs.com) (BSD-2-Clause, `server/web/vendor/leaflet/LICENSE`) ·
[osmdroid](https://github.com/osmdroid/osmdroid) (Apache-2.0) ·
[BRouter](https://github.com/abrensch/brouter) (MIT) · Ortssuche [Nominatim](https://nominatim.org)
