package br.com.exotermo.hermes.app.execution;

import java.util.UUID;
import org.springframework.data.jpa.repository.JpaRepository;

interface HermesExecutionRepository extends JpaRepository<HermesExecution, UUID> { }
