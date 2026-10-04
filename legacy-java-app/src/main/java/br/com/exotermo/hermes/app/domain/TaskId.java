package br.com.exotermo.hermes.app.domain;
import java.util.UUID;
public record TaskId(UUID value) { public static TaskId newId() { return new TaskId(UUID.randomUUID()); } }
