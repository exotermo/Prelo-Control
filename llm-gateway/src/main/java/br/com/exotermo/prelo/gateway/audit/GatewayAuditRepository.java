package br.com.exotermo.prelo.gateway.audit;

import java.util.UUID;
import org.springframework.data.jpa.repository.JpaRepository;

interface GatewayAuditRepository extends JpaRepository<GatewayAuditEvent, UUID> { }
