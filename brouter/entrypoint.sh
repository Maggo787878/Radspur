#!/bin/sh
# Lädt fehlende bzw. veraltete Kartenkacheln und startet den BRouter-Server.
set -eu
SEG_DIR="${SEG_DIR:-/segments}"
BASE="${BROUTER_SEGMENTS_URL:-https://brouter.de/brouter/segments4}"

for s in $BROUTER_SEGMENTS; do
  case "$s" in
    [EW][0-9]*_[NS][0-9]*) ;;
    *) echo "Ungültiger Segmentname: $s" >&2; exit 1 ;;
  esac
  f="$SEG_DIR/$s.rd5"
  if [ ! -s "$f" ] || [ -n "$(find "$f" -mtime +"$BROUTER_REFRESH_DAYS" 2>/dev/null)" ]; then
    echo "Lade Kartendaten $s …"
    if wget -q -O "$f.tmp" "$BASE/$s.rd5" && [ -s "$f.tmp" ]; then
      mv "$f.tmp" "$f"
      echo "  $s: $(du -h "$f" | cut -f1)"
    else
      rm -f "$f.tmp"
      if [ -s "$f" ]; then echo "  Aktualisierung fehlgeschlagen, verwende vorhandene Daten" >&2
      else echo "  Download von $s fehlgeschlagen" >&2; exit 1; fi
    fi
  fi
done

echo "Starte BRouter (Speicher $BROUTER_MEMORY, Segmente: $BROUTER_SEGMENTS)"
exec java -Xmx"$BROUTER_MEMORY" -Xms128m -cp /opt/brouter/brouter.jar \
  btools.server.RouteServer "$SEG_DIR" /opt/brouter/profiles2 /opt/brouter/customprofiles 17777 "$BROUTER_THREADS"
