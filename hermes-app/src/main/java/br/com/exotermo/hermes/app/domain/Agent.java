package br.com.exotermo.hermes.app.domain;
public record Agent(AgentId id, AgentType type) { public static Agent general() { return new Agent(new AgentId("general"), AgentType.GENERAL); } }
