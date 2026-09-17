package br.com.exotermo.hermes.app.domain;
import java.util.UUID;
public record ExecutionId(UUID value) { public static ExecutionId newId() { return new ExecutionId(UUID.randomUUID()); } }
