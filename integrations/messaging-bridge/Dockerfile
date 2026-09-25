FROM maven:3.9-eclipse-temurin-21-alpine AS build
WORKDIR /workspace
COPY pom.xml .
COPY src src
RUN mvn -B package -DskipTests

FROM eclipse-temurin:21-jre-alpine
RUN addgroup -S bridge && adduser -S bridge -G bridge
WORKDIR /app
COPY --from=build --chown=bridge:bridge /workspace/target/hermes-messaging-bridge-*.jar app.jar
USER bridge
EXPOSE 8095
ENTRYPOINT ["java", "-jar", "/app/app.jar"]
