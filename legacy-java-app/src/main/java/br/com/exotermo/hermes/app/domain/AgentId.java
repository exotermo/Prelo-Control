package br.com.exotermo.hermes.app.domain;
public record AgentId(String value) { public AgentId { if (value == null || value.isBlank()) throw new IllegalArgumentException("agent id is required"); } }
