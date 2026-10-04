package br.com.exotermo.hermes.app.domain;
public class UnknownAgentException extends RuntimeException { public UnknownAgentException(AgentId agentId) { super("unknown agent: " + agentId.value()); } }
