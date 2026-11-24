# Radspur

Selbst gehostetes Handy-Finden und Tour-Tracking (Radtouren) – Android-App plus kleiner Server als Docker-Container.

```
android/   Android-App (Kotlin, Jetpack Compose, OpenStreetMap)
server/    Server (Go, SQLite, Weboberfläche mit Leaflet)
deploy/    docker-compose.yml, .env.example, nginx-Vorlage
.github/   CI: baut das Server-Image (GHCR) und die APK
```

## Funktionen

**App**
- Tour aufzeichnen (Foreground-Service, GPS alle 1–2 s) mit Live-Karte
- Distanz, Fahrzeit, Ø- und Max.-Geschwindigkeit, Höhenmeter
- Pause/Weiter; Aufzeichnung übersteht einen Neustart der App
- Tourliste, Detailansicht mit Karte, GPX teilen
- Fertige Touren automatisch zum Server hochladen
- Handy-Ortung: meldet alle 15 min Position + Akkustand an den Server und führt Befehle aus

**Server / Web**
- Letzte Position, Genauigkeit, Akku, Verlauf (6 h / 24 h / 7 Tage) auf der Karte
- **Klingeln lassen** (volle Lautstärke über den Alarm-Kanal, auch im Lautlos-Modus, 2 min oder bis Stopp)
- **Position anfordern** (frischer GPS-Fix mit hoher Genauigkeit)
- Nach einem Befehl meldet sich das Handy 15 min lang jede Minute („Suchmodus“)
- Touren ansehen, GPX herunterladen, löschen
- Positionsverlauf wird nach `LOCATION_RETENTION_DAYS` gelöscht

## Server betreiben

Der Container spricht reines HTTP auf Port 8080 und ist für den Betrieb hinter **nginx** gedacht.

```bash
git clone https://github.com/Maggo787878/Radspur.git && cd Radspur/deploy
cp .env.example .env
openssl rand -hex 32     # → DEVICE_TOKEN in .env
nano .env                # ADMIN_PASSWORD, DEVICE_TOKEN, ggf. RADSPUR_BIND
docker compose pull
docker compose up -d
```

### nginx davor

| nginx läuft … | `.env` | Ziel im Proxy |
|---|---|---|
| auf demselben Host | `RADSPUR_BIND=127.0.0.1` | `http://127.0.0.1:8080` |
| auf einem anderen Rechner | `RADSPUR_BIND=0.0.0.0` | `http://<docker-host>:8080` |
| als Container (z. B. Nginx Proxy Manager) | `PROXY_NETWORK=<netz>` und `docker compose -f docker-compose.yml -f docker-compose.npm.yml up -d` | `http://radspur:8080` |

Eine fertige Vorlage liegt in `deploy/nginx/radspur.conf`. Wichtig sind darin:

- `client_max_body_size 32m;` – sonst lehnt nginx Tour-Uploads über 1 MB mit `413` ab.
- `X-Forwarded-Proto $scheme` – so weiß der Server, ob der Browser HTTP oder HTTPS nutzt (Login-Cookie), und
  `X-Real-IP` für die echte Client-IP im Log (`docker logs radspur`).

Bei Nginx Proxy Manager: unter *Advanced* `client_max_body_size 32m;` eintragen.

Reines HTTP funktioniert für Weboberfläche und App. Passwort, Geräte-Token und Positionen gehen dann aber
im Klartext über das Netz – über das Internet daher besser HTTPS (z. B. certbot, Vorlage ebenfalls in der Datei)
oder Zugriff nur über VPN.

Die Daten liegen im Volume `radspur-data` (SQLite), Backup z. B.:

```bash
docker run --rm -v deploy_radspur-data:/data -v "$PWD":/backup alpine tar czf /backup/radspur-data.tgz -C /data .
```

Update: `docker compose pull && docker compose up -d`

### Image aus GHCR

Jeder Push auf `main` (Änderungen in `server/`) baut `ghcr.io/<owner>/radspur-server:latest` für amd64 und arm64.
Ist das Repository privat, ist auch das Paket privat – dann entweder

- auf dem Server einmalig `docker login ghcr.io -u <github-user>` mit einem Personal Access Token (Scope `read:packages`), oder
- unter GitHub → Packages → radspur-server → Package settings die Sichtbarkeit auf *public* stellen.

## App installieren

Die APK baut GitHub Actions bei jedem Push in `android/` (Artefakt `radspur-apk`), bei einem Tag `v*` zusätzlich als Release.

**Wichtig – Signatur:** Damit Updates über eine bestehende Installation gehen, muss jede APK mit demselben Schlüssel signiert sein. Einmalig erzeugen:

```bash
keytool -genkeypair -v -keystore radspur.jks -alias radspur -keyalg RSA -keysize 4096 -validity 10000
```

```bash
base64 -w0 radspur.jks
```

Als Repository-Secrets anlegen (Settings → Secrets and variables → Actions):
`RADSPUR_KEYSTORE_BASE64`, `RADSPUR_KEYSTORE_PASSWORD`, `RADSPUR_KEY_ALIAS` (`radspur`), `RADSPUR_KEY_PASSWORD`.
Den Keystore sicher aufbewahren – ohne ihn sind keine Updates möglich.

### Einrichtung am Handy

1. APK installieren („Unbekannte Apps installieren“ für Browser/Dateimanager erlauben).
2. **Einstellungen**: Server-URL (`http://…` oder `https://…`) und `DEVICE_TOKEN` eintragen → *Speichern & testen*.
3. Berechtigungen: Standort, Benachrichtigungen, **Standort im Hintergrund („Immer zulassen“)**, **Akku-Optimierung deaktivieren**.
4. Herstellerspezifisch (Samsung, Xiaomi, Huawei …): App vom „Energiesparen/Autostart-Verwaltung“ ausnehmen, sonst stoppt das System die Hintergrundmeldungen.

## Grenzen (Version 1)

- Android plant periodische Hintergrundjobs frühestens alle 15 min, im Doze-Modus auch seltener. Ein Befehl aus dem Web wird daher erst beim nächsten Check-in ausgeführt (typisch bis 15 min, manchmal länger). Danach reagiert das Handy minütlich.
  Ausbaustufe: Push über einen selbst gehosteten **ntfy**-Server (UnifiedPush) für sofortige Reaktion.
- Ein Benutzer, ein Gerät.
- Die App erlaubt HTTP und HTTPS; bei `http://` zeigt sie in den Einstellungen eine Warnung.
- Ausgeschaltetes Handy oder Handy ohne Netz kann nicht gefunden werden – die zuletzt gemeldete Position bleibt sichtbar.

## API

| Methode | Pfad | Auth | Zweck |
|---|---|---|---|
| POST | `/api/device/locations` | Bearer `DEVICE_TOKEN` | Positionen melden, Antwort: offene Befehle |
| POST | `/api/device/commands/{id}/ack` | Bearer | Befehl bestätigen |
| POST | `/api/device/tours` | Bearer | Tour hochladen (Upsert per `clientId`) |
| GET | `/healthz` | – | Healthcheck |

Die Web-API (`/api/web/*`) nutzt ein Session-Cookie nach Login mit `ADMIN_PASSWORD`.

## Lokal entwickeln

```bash
cd server && ADMIN_PASSWORD=testtest DEVICE_TOKEN=0123456789abcdef DB_PATH=./dev.db SECURE_COOKIE=false go run .
```

```bash
cd android && ./gradlew assembleDebug
```
