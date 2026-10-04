FROM maven:3.9-eclipse-temurin-21-alpine AS build
WORKDIR /workspace
COPY pom.xml .
COPY src src
RUN mvn -B package

FROM eclipse-temurin:21-jre-alpine
RUN addgroup -S hermes && adduser -S hermes -G hermes
WORKDIR /app
COPY --from=build --chown=hermes:hermes /workspace/target/hermes-app-*.jar app.jar
USER hermes
EXPOSE 8080
ENTRYPOINT ["java", "-jar", "/app/app.jar"]
