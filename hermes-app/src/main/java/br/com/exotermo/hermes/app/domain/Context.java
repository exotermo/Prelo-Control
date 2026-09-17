package br.com.exotermo.hermes.app.domain;
import java.util.List;
public record Context(List<ContextItem> items) { public Context { items = List.copyOf(items); } }
