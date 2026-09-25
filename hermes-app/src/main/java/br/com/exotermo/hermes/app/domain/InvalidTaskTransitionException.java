package br.com.exotermo.hermes.app.domain;
public class InvalidTaskTransitionException extends RuntimeException { public InvalidTaskTransitionException(TaskStatus from, TaskStatus to) { super("cannot transition task from " + from + " to " + to); } }
