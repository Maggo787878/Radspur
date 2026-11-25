# BRouter – Routenberechnung für Rad und Fuß (https://github.com/abrensch/brouter)
FROM eclipse-temurin:21-jre-alpine
ARG BROUTER_VERSION=1.7.9
RUN wget -q -O /tmp/brouter.zip "https://github.com/abrensch/brouter/releases/download/v${BROUTER_VERSION}/brouter-${BROUTER_VERSION}.zip" \
 && unzip -q /tmp/brouter.zip -d /tmp \
 && mkdir -p /opt/brouter/customprofiles /segments \
 && mv "/tmp/brouter-${BROUTER_VERSION}/brouter-${BROUTER_VERSION}-all.jar" /opt/brouter/brouter.jar \
 && mv "/tmp/brouter-${BROUTER_VERSION}/profiles2" /opt/brouter/profiles2 \
 && rm -rf /tmp/brouter* \
 && adduser -D -u 10002 brouter && chown brouter /segments /opt/brouter/customprofiles
COPY --chmod=755 entrypoint.sh /usr/local/bin/entrypoint.sh
USER brouter
VOLUME /segments
EXPOSE 17777
# Kartenkacheln (5x5 Grad). Standard: Österreich mit Grenzgebieten.
ENV BROUTER_SEGMENTS="E5_N45 E10_N45 E15_N45" \
    BROUTER_REFRESH_DAYS=30 \
    BROUTER_MEMORY=1g \
    BROUTER_THREADS=4
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
