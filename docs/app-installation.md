# Radspur-App installieren

Die App gibt es für **Android ab Version 8.0**. Sie wird nicht über den Play Store verteilt, sondern als
Datei (`radspur.apk`) direkt von GitHub installiert. Für iPhones gibt es derzeit keine App – siehe
[iPhone](#iphone).

**Voraussetzung:** ein Radspur-Konto. Konten legt der Administrator per Einladungslink an – ohne Einladung
ist keine Registrierung möglich.

## 1. Konto anlegen

1. Den Einladungslink vom Administrator öffnen (gültig 7 Tage, nur einmal verwendbar).
2. **Benutzername** wählen (3–32 Zeichen: Buchstaben, Ziffern, `.` `_` `-`) und ein **Passwort** mit
   mindestens 10 Zeichen festlegen.
3. Danach bist du in der Weboberfläche angemeldet. Benutzername und Passwort brauchst du gleich in der App.

## 2. App herunterladen und installieren

1. Am Handy die Seite **https://github.com/Maggo787878/Radspur/releases/latest** öffnen.
2. Unter *Assets* auf **`radspur.apk`** tippen – die Datei wird heruntergeladen.
3. Die heruntergeladene Datei öffnen (Benachrichtigung *Download abgeschlossen* antippen oder
   *Dateien → Downloads*).
4. Beim ersten Mal fragt Android, ob der Browser **Apps installieren** darf:
   **Einstellungen** antippen → **Dieser Quelle vertrauen** / *Aus dieser Quelle zulassen* einschalten →
   zurück.
   (Später findest du das unter *Einstellungen → Apps → Spezieller App-Zugriff → Unbekannte Apps
   installieren*.)
5. **Installieren** antippen.
6. Warnt **Google Play Protect** („App wurde nicht von Play geprüft“ o. ä.): *Weitere Details* →
   **Trotzdem installieren**. Die Warnung erscheint, weil die App nicht aus dem Play Store kommt.

## 3. In der App anmelden

1. Radspur öffnen → unten **Einstellungen**.
2. Eintragen:
   - **Server-URL:** `https://radspur.mwonline.at`
   - **Benutzername** und **Passwort** aus Schritt 1
3. **Anmelden** antippen. Nach ein paar Sekunden erscheint *Letzte Meldung an Server* mit Uhrzeit.

Das Passwort wird nicht am Handy gespeichert; die App erhält ein eigenes Geräte-Token. In der
Weboberfläche unter **Konto → Angemeldete Handys** siehst du alle angemeldeten Geräte und kannst sie
entfernen.

## 4. Berechtigungen erteilen

Ebenfalls unter **Einstellungen → Berechtigungen** – bei jedem Punkt mit ✗ auf **Erlauben** tippen:

| Berechtigung | Wofür | Hinweis |
|---|---|---|
| **Standort** | Touren aufzeichnen | „Bei Nutzung der App zulassen“ genügt dafür |
| **Benachrichtigungen** | Anzeige während der Aufzeichnung, Klingeln beim Handy-Finden | |
| **Standort im Hintergrund** | Handy finden | Android öffnet die Einstellungen → **Immer zulassen** wählen |
| **Akku-Optimierung deaktivieren** | Handy finden zuverlässig | Dialog mit **Zulassen** bestätigen |

Ohne „Standort im Hintergrund“ und ohne deaktivierte Akku-Optimierung funktioniert das Aufzeichnen
trotzdem – nur „Handy finden“ meldet sich dann unregelmäßig oder gar nicht.

### Herstellerspezifische Energiesparfunktionen

Viele Hersteller beenden Hintergrund-Apps zusätzlich. Je nach Gerät und Version heißen die Menüs etwas
anders:

- **Samsung:** *Einstellungen → Akku → Hintergrundnutzungslimits → Nie im Standby befindliche Apps* →
  Radspur hinzufügen.
- **Xiaomi / Redmi / POCO:** *App-Info von Radspur → Akkusparmodus → Keine Einschränkungen*, außerdem
  *Autostart* einschalten.
- **Huawei / Honor:** *Einstellungen → Akku → App-Start → Radspur* → *Manuell verwalten*, alle drei
  Schalter ein.
- **OnePlus / OPPO / realme:** *App-Info → Akkunutzung → Hintergrundaktivität zulassen*.
- **Google Pixel:** keine zusätzlichen Schritte nötig.

Anleitungen für weitere Geräte: https://dontkillmyapp.com

## Benutzung in Kürze

- **Aufzeichnen:** oben Rad oder Laufen wählen → *Radtour starten* bzw. *Lauf starten*. Die Aufzeichnung läuft auch bei gesperrtem
  Bildschirm weiter. *Beenden* speichert die Tour und lädt sie zum Server hoch.
- **Touren:** Liste und Details mit Karte, GPX teilen.
- **Statistik:** Woche, Monat, Jahr, gesamt; Export als Excel-Datei.
- **Weboberfläche** (`https://radspur.mwonline.at`): Handy orten und klingeln lassen, Touren und Statistik,
  Routen planen und als GPX für den Radcomputer herunterladen, Touren nachtragen.

## Updates

1. Neue `radspur.apk` von **https://github.com/Maggo787878/Radspur/releases/latest** herunterladen.
2. Öffnen → **Aktualisieren** bzw. *Installieren*.

Die neue Version wird **über die vorhandene installiert** – Touren und Anmeldung bleiben erhalten.
**Nicht vorher deinstallieren**, sonst gehen Touren verloren, die nur am Handy liegen.

Welche Version installiert ist: *Einstellungen → Apps → Radspur* (unten *Version*).

## Probleme

| Problem | Lösung |
|---|---|
| *App nicht installiert* / *Paket steht in Konflikt mit einem vorhandenen Paket* | Die installierte Version ist mit einem anderen Schlüssel signiert (z. B. eine frühe Testversion). Vorher in der App unter **Touren** prüfen, dass alle Touren hochgeladen sind (Wolke mit Haken), dann alte App deinstallieren und neu installieren. |
| *Paket ist ungültig* / Datei lässt sich nicht öffnen | Download unvollständig – Datei löschen und erneut herunterladen. |
| *Anmeldung fehlgeschlagen: Benutzername oder Passwort falsch* | Zugangsdaten prüfen. Passwort vergessen → Administrator um einen Passwort-Link bitten. |
| *Zu viele Fehlversuche* | Nach 5 Fehlversuchen ist die Anmeldung 15 Minuten gesperrt. |
| *Nicht angemeldet oder Konto gesperrt* | In den Einstellungen abmelden und neu anmelden; hilft das nicht, Administrator fragen. |
| Touren werden nicht hochgeladen | Internetverbindung prüfen; in der Tour auf *Hochladen* tippen. Fehler stehen unter *Einstellungen*. |
| Handy meldet sich selten | Berechtigungen und Energiesparfunktionen prüfen (Schritt 4). Android meldet höchstens alle 15 Minuten. |

## Deinstallieren

*Einstellungen → Apps → Radspur → Deinstallieren*. Touren, die nur am Handy gespeichert sind, gehen dabei
verloren; hochgeladene Touren bleiben am Server und in der Weboberfläche. Das Gerät verschwindet nicht
automatisch aus der Liste der angemeldeten Handys – in der Weboberfläche unter **Konto** entfernen.

## iPhone

Eine iOS-App gibt es derzeit nicht. Am iPhone funktioniert die **Weboberfläche** in Safari vollständig
(Touren, Statistik, Routenplanung, GPX-Export). Zum Wiederfinden des iPhones bitte Apples eingebaute
Funktion **„Wo ist?“** verwenden.

---

## Für den Administrator

### Neue Person einladen

1. Weboberfläche → **Benutzer** → Notiz eintragen (z. B. den Namen) → **Einladungslink erstellen** →
   **Kopieren**.
2. Link zusammen mit dieser Anleitung verschicken, z. B.:

   > Hallo! Hier ist deine Einladung zu Radspur (7 Tage gültig): **‹Link›**
   > Anleitung zur App: https://github.com/Maggo787878/Radspur/blob/main/docs/app-installation.md
   > Server-URL in der App: `https://radspur.mwonline.at`

### Neue App-Version veröffentlichen

1. https://github.com/Maggo787878/Radspur/releases/new öffnen.
2. **Choose a tag** → neue Versionsnummer eintippen, z. B. `v0.5.0` → *Create new tag on publish*.
3. Titel z. B. `Radspur 0.5.0`, optional Änderungen beschreiben → **Publish release**.
4. Nach 3–4 Minuten hängt `radspur.apk` am Release. GitHub prüft dabei automatisch, dass die App mit dem
   richtigen Schlüssel signiert ist; nur dann wird sie veröffentlicht.

Die Signatur-Secrets im Repository (`RADSPUR_KEYSTORE_*`) und die Sicherung von `radspur.jks` samt Passwort
müssen erhalten bleiben – ohne sie lassen sich keine Updates mehr über die installierte App installieren.
