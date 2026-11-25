# Kartendaten für die Routenplanung

Die Routenplanung im Web (Reiter **Routen**) berechnet Strecken mit [BRouter](https://github.com/abrensch/brouter)
im Container `radspur-brouter`. BRouter braucht dafür Kartendaten der Region, aufbereitet aus
OpenStreetMap. Diese Anleitung zeigt, wie du Regionen **hinzufügst**, **aktualisierst** und **entfernst**.

> Die Kartendaten betreffen nur die Routen**berechnung**. Die angezeigte Karte im Browser kommt
> unabhängig davon von OpenStreetMap und zeigt immer die ganze Welt.

## So sind die Kartendaten aufgeteilt

Die Daten liegen in **Kacheln zu 5 × 5 Grad**. Der Name nennt die linke untere (südwestliche) Ecke:

| Kachel | deckt ab |
|---|---|
| `E10_N45` | 10° bis 15° Ost, 45° bis 50° Nord |
| `E15_N45` | 15° bis 20° Ost, 45° bis 50° Nord |
| `W5_N40` | 5° bis 0° West, 40° bis 45° Nord |

Eine Route lässt sich nur berechnen, wenn **alle** ihre Punkte in geladenen Kacheln liegen. Sonst meldet die
Routenplanung: *„Ein Punkt liegt außerhalb der geladenen Kartendaten“*.

Die Daten kommen von [brouter.de](https://brouter.de/brouter/segments4/) und werden dort **täglich** aus
OpenStreetMap neu erzeugt.

### Welche Kachel brauche ich?

Koordinaten eines Ortes ermitteln: z. B. in OpenStreetMap oder Google Maps mit Rechtsklick auf den Ort, die
erste Zahl ist die Breite (Nord), die zweite die Länge (Ost). Dann **beide Zahlen auf das nächstkleinere
Vielfache von 5 abrunden**:

- Graz: 47,07 N / 15,44 O → `E15_N45`
- Dresden: 51,05 N / 13,74 O → `E10_N50`
- Split: 43,51 N / 16,44 O → `E15_N40`

Oder direkt am Server ausrechnen lassen (Breite, Länge):

```bash
echo "47.07 15.44" | awk '{lat=$1; lon=$2; la=int(lat/5)*5; if (lat<0 && lat!=la) la-=5; lo=int(lon/5)*5; if (lon<0 && lon!=lo) lo-=5; printf "%s%d_%s%d\n", (lo<0?"W":"E"), (lo<0?-lo:lo), (la<0?"S":"N"), (la<0?-la:la)}'
```

### Kacheln für häufige Regionen

Größen laut brouter.de (Stand November 2026), sie ändern sich mit den OSM-Daten leicht.

| Region | Kacheln | Download |
|---|---|---|
| **Österreich mit Grenzgebieten** (Standard; inkl. Schweiz, Südtirol, Slowenien, Bayern-Süd) | `E5_N45 E10_N45 E15_N45` | ca. 590 MB |
| + Deutschland (fast vollständig) | `E5_N50 E10_N50` | + 310 MB |
| + Deutschland vollständig (Görlitz, Sylt) | `E15_N50 E5_N55` | + 125 MB |
| + Tschechien | `E10_N50 E15_N50` | + 220 MB |
| + Ungarn (Osten) | `E20_N45` | + 70 MB |
| + Kroatien (Küste, Dalmatien) | `E10_N40 E15_N40` | + 135 MB |
| + Norditalien bis Toskana | `E5_N40 E10_N40` | + 150 MB |

Beispiel Österreich + Deutschland + Kroatien:

```
BROUTER_SEGMENTS=E5_N45 E10_N45 E15_N45 E5_N50 E10_N50 E10_N40 E15_N40
```

## Weitere Regionen hinzufügen

Alle Befehle im Ordner mit deiner `docker-compose.yml` und `.env` ausführen.

1. In `.env` die Kacheln an `BROUTER_SEGMENTS` anhängen, durch **Leerzeichen** getrennt:

   ```
   BROUTER_SEGMENTS=E5_N45 E10_N45 E15_N45 E5_N50 E10_N50
   ```

2. Den Container mit der neuen Einstellung neu erstellen:

   ```bash
   docker compose up -d brouter
   ```

3. Download beobachten – fehlende Kacheln werden geladen, vorhandene bleiben:

   ```bash
   docker logs -f radspur-brouter
   ```

   Fertig, sobald `Starte BRouter` und `BRouter 1.7.9` erscheinen (Strg+C beendet nur die Anzeige).
   Während des Downloads ist die Routenplanung nicht verfügbar
   (*„Routenberechnung startet noch“*).

**Speicher:** Der Platz auf der Festplatte wächst mit den Kacheln (siehe Tabelle). Der Arbeitsspeicher
kaum – BRouter liest Kacheln bei Bedarf. `BROUTER_MEMORY=1g` reicht für normale Tagestouren auch mit vielen
Kacheln. Bei sehr langen Routen (mehrere hundert Kilometer) und Fehlern im Log wie `OutOfMemoryError`
auf `2g` erhöhen und `docker compose up -d brouter` ausführen.

## Kartendaten aktualisieren

### Automatisch

Beim **Start** prüft der Container jede Kachel: Ist sie älter als `BROUTER_REFRESH_DAYS` Tage
(Standard **30**), wird sie neu geladen. Schlägt der Download fehl, läuft BRouter mit den alten Daten
weiter.

Weil das nur beim Start passiert, braucht ein dauerhaft laufender Container einen regelmäßigen Neustart.
Am einfachsten per Cron, z. B. am 1. jedes Monats um 4:00 Uhr (`crontab -e`):

```
0 4 1 * * docker restart radspur-brouter
```

Der Neustart samt Download dauert einige Minuten; in der Zeit ist nur die Routenplanung nicht verfügbar.

### Sofort, alle Kacheln

```bash
docker exec radspur-brouter sh -c 'rm -f /segments/*.rd5' && docker restart radspur-brouter
```

### Sofort, einzelne Kachel

```bash
docker exec radspur-brouter rm /segments/E10_N45.rd5 && docker restart radspur-brouter
```

### Stand prüfen

```bash
docker exec radspur-brouter ls -lh /segments
```

Datum und Größe jeder geladenen Kachel. Zum Vergleich der aktuelle Stand auf
https://brouter.de/brouter/segments4/.

### Seltener oder häufiger aktualisieren

In `.env` z. B. `BROUTER_REFRESH_DAYS=7` für wöchentlich (dann auch den Cron-Neustart wöchentlich), danach
`docker compose up -d brouter`. Für Radtouren genügen monatliche Daten in aller Regel – neue Radwege und
Sperren tauchen in OpenStreetMap ohnehin erst nach und nach auf.

## Regionen entfernen

1. Kachel aus `BROUTER_SEGMENTS` in `.env` streichen.
2. Datei löschen und Container neu erstellen:

   ```bash
   docker exec radspur-brouter rm /segments/E5_N50.rd5 && docker compose up -d brouter
   ```

Bereits gespeicherte Routen bleiben erhalten (sie sind am Radspur-Server gespeichert, nicht in BRouter),
lassen sich in dieser Region aber nicht mehr neu berechnen.

## Komplett neu laden

Wenn das Volume beschädigt ist oder du alles zurücksetzen willst:

```bash
docker compose stop brouter && docker compose rm -f brouter
```

```bash
docker volume ls | grep brouter-segments
```

```bash
docker volume rm <name-aus-der-liste>
```

```bash
docker compose up -d brouter
```

Der Volumenname beginnt mit dem Namen deines Compose-Ordners, z. B. `deploy_brouter-segments`.

## Fehlersuche

| Meldung in der Routenplanung | Ursache | Lösung |
|---|---|---|
| *Routenberechnung startet noch* | Kacheln werden geladen | warten, `docker logs -f radspur-brouter` |
| *BRouter-Container nicht gefunden* | Container fehlt oder alte `docker-compose.yml` | aktuelle `docker-compose.yml` verwenden, `docker compose up -d` |
| *Ein Punkt liegt außerhalb der geladenen Kartendaten* | Kachel fehlt | Kachel ergänzen (siehe oben) |
| *Keine Route gefunden* | Punkt liegt abseits von Wegen, oder das Profil erlaubt den Weg nicht (z. B. Rennrad auf Schotter) | Punkt näher an einen Weg setzen oder anderes Profil wählen |
| *antwortet nicht rechtzeitig* | sehr lange Route oder Server ausgelastet | Route in Etappen planen, ggf. `BROUTER_MEMORY` erhöhen |

Im Log von `radspur-brouter`:

| Log-Zeile | Bedeutung |
|---|---|
| `Download von … fehlgeschlagen` | Server erreicht `brouter.de` nicht (Firewall, DNS). Test: `curl -sI https://brouter.de/brouter/segments4/` |
| `Ungültiger Segmentname` | Tippfehler in `BROUTER_SEGMENTS` (Format `E10_N45`) |
| `Aktualisierung fehlgeschlagen, verwende vorhandene Daten` | Update nicht möglich, alte Daten bleiben aktiv |
